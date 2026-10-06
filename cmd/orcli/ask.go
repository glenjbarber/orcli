package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tools"
	"github.com/glenjbarber/orcli/internal/tui"
)

// maxToolRounds bounds how many times one turn can call a tool before this gives up
// and reports the turn stopped rather than looping forever against a model that keeps
// asking for more.
//
// Eight is a judgment, not a derivation: enough for a real multi-step task (read a
// file, edit it, run a test, check the result) without being indistinguishable from a
// hang.
const maxToolRounds = 8

// chatClient is the one method ask needs from *openrouter.Client.
//
// It exists so a test can hand ask a fake that answers in memory, since
// openrouter.Client's base URL is fixed by New and cannot be pointed at a test
// server from outside the package. *openrouter.Client already satisfies this with no
// change to it.
type chatClient interface {
	Chat(ctx context.Context, req openrouter.Request, onEvent func(openrouter.Event)) error
}

// ask sends a question to the model, running any tools it calls along the way, and
// writes what comes back into the session.
//
// It is here rather than in internal/tui for the reason the dispatcher is:
// internal/tui holds no credential and reaches for no client, so something outside
// it has to hold the transport, the tools, and the session together. This is that
// something, and it is the only place they meet.
//
// # A turn is the connection probe
//
// The reader's own first question is the probe. There is no `/connect` command and
// no separate check: a turn that comes back with text has proved the credential and
// the model together, and that is what confirms the model was good before anything
// is written to the configuration file.
//
// # Tools, not yet session state
//
// A turn now carries real tools - filesystem, git, shell, all from internal/tools,
// contained to the session's own working directory - and loops, replaying the
// model's own tool calls and their results, until it answers with no further call or
// maxToolRounds is reached. What it still does not carry is history: each question is
// its own conversation, with no memory of a turn before it. That is adr-0000006's
// queue and every record built on a running conversation left exactly as open as it
// was; this closes the separate, narrower gap ask.go's own doc comment used to name -
// "no tools are offered" - without deciding the first.
//
// # The introduction
//
// If CONTEXT.md exists in the session's working directory, its contents are sent as
// one system message ahead of the question, naming it as the reader's own
// introduction rather than inventing one. This is narrower than adr-0000042's
// capability message, which wants a list assembled from live session state
// (Options, Ready, connector checks) rather than a static file's contents; the two
// are not the same thing, and this does not implement that record.
func ask(s *tui.Session, c chatClient, attribution string, confirmed func(model string)) tui.AskFunc {
	toolset := newToolset(s.Options().WorkingDir)
	schemas := toolSchemas(toolset)
	intro := introduction(s.Options().WorkingDir)

	return func(ctx context.Context, question string, level int) error {
		turnCtx, err := s.Begin(ctx, question, level)
		if err != nil {
			return err
		}

		// The model is read through the session rather than off the options value,
		// since `/model` can change it while a turn is in flight and the turn has to
		// be sent with the model in force when it was asked rather than the one the
		// file happened to hold when the request was built.
		model := s.Options().Model
		approval := string(s.Options().Approval)

		messages := make([]openrouter.Message, 0, 3)
		if intro != "" {
			messages = append(messages, openrouter.Message{Role: "system", Content: intro})
		}
		messages = append(messages, openrouter.Message{Role: "user", Content: question})

		for round := 0; round < maxToolRounds; round++ {
			var reply strings.Builder
			var calls []openrouter.ToolCall
			var reason string
			var finished bool
			var failed error

			err := c.Chat(turnCtx, openrouter.Request{
				Model:         model,
				Messages:      messages,
				Tools:         schemas,
				Stream:        true,
				AttributionID: attribution,
			}, func(e openrouter.Event) {
				switch e.Kind {
				case openrouter.EventDelta:
					reply.WriteString(e.Text)

				case openrouter.EventTool:
					if e.ToolCall != nil {
						calls = append(calls, *e.ToolCall)
					}

				case openrouter.EventError:
					// The text that arrived before a failure is kept and the
					// failure is reported beside it, since the text is usually
					// more useful than the error alone and a reader handed only
					// an error has nothing to read.
					s.Deliver(reply.String(), level)
					if e.Err != nil {
						s.Notice(e.Err.Error(), level, tui.RoleFailure)
						failed = e.Err
					}

				case openrouter.EventFinish:
					reason = e.Reason
					finished = e.Finished
				}
			})
			if err != nil {
				s.Notice(err.Error(), level, tui.RoleFailure)
				s.Finished("failed")
				return nil
			}
			if failed != nil {
				return nil
			}

			if len(calls) == 0 {
				// Deliver is a no-op for an empty reply, so a turn that failed
				// before any text does not write a blank row.
				s.Deliver(reply.String(), level)
				s.Finished(reason)

				// A reply with text in it is the confirmation, and an empty one
				// is a refusal even when the endpoint called the turn finished.
				// A stream that ended without delivering anything has not
				// proved the credential works, and writing the model on that
				// evidence would record a model nobody has reached the
				// endpoint with.
				if confirmed != nil && reply.Len() > 0 && finished {
					confirmed(model)
				}
				return nil
			}

			// The model wants to call tools. Its own message, carrying the calls,
			// goes back in exactly as the endpoint sent it, then one tool-role
			// message per call, each paired to its call by ID - the shape the
			// endpoint expects a round that called tools to be replayed in.
			messages = append(messages, openrouter.Message{
				Role:      "assistant",
				Content:   reply.String(),
				ToolCalls: calls,
			})
			for _, call := range calls {
				messages = append(messages, openrouter.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    runTool(toolset, approval, call),
				})
			}
		}

		s.Notice(fmt.Sprintf("stopped after %d rounds of tool calls", maxToolRounds), level, tui.RoleFailure)
		s.Finished("stopped")
		return nil
	}
}

// newToolset builds the tools a turn may call, contained to dir.
//
// Filesystem tools are omitted, not fatal, if the root cannot be opened - a session
// without them is a session with less in it rather than one that cannot run, the same
// rule NewFilesystem's own doc comment states for its caller.
func newToolset(dir string) []tools.Tool {
	set := []tools.Tool{tools.NewGit(dir), tools.NewShell(dir)}
	if fs, err := tools.NewFilesystem(dir); err == nil {
		set = append(set, fs.Tools()...)
	}
	return set
}

// toolSchemas converts a toolset into the shape a request offers the model.
func toolSchemas(toolset []tools.Tool) []openrouter.Tool {
	schemas := make([]openrouter.Tool, 0, len(toolset))
	for _, t := range toolset {
		d := t.Describe()
		schemas = append(schemas, openrouter.Tool{
			Type: d.Type,
			Function: openrouter.ToolFunction{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				Parameters:  d.Function.Parameters,
			},
		})
	}
	return schemas
}

// runTool finds the tool a call named and runs it, refreshing the approval mode each
// call rather than once at construction, since `/approval` can change it between
// turns and a tool built at startup would otherwise answer to a mode the reader left
// behind.
//
// It always returns text for the tool-role message, never an error the caller has to
// handle specially - a call that failed, named a tool that does not exist, or carried
// arguments that were not a JSON object all become a message telling the model so,
// which is the same guarantee internal/tools.invoke makes for a Run call alone,
// carried one layer out to a call this package cannot find at all.
func runTool(toolset []tools.Tool, approval string, call openrouter.ToolCall) string {
	for _, t := range toolset {
		if t.Name() != call.Function.Name {
			continue
		}
		switch c := t.(type) {
		case *tools.Git:
			c.Approval = approval
		case *tools.Shell:
			c.Approval = approval
		}
		result := t.Run(json.RawMessage(call.Function.Arguments))
		if result.Err != nil {
			return result.Err.Error()
		}
		return result.Content
	}
	return fmt.Sprintf("tools: no tool named %q", call.Function.Name)
}

// introduction reads CONTEXT.md from dir, for the one system message a turn sends
// ahead of the question. A missing file is silence, not a failure: most working
// directories have none, and a session without one sends no introduction at all
// rather than an empty one.
func introduction(dir string) string {
	text, err := os.ReadFile(filepath.Join(dir, "CONTEXT.md"))
	if err != nil {
		return ""
	}
	return string(text)
}

// canAsk reports whether a model can be asked anything at all.
//
// It is the dispatcher's answer to a question it needs before it can choose between
// sending its guidance to the model and falling back to its own text: a reader with
// no credential and no model chosen cannot be answered by a model, and /cloudflare
// must still produce a result rather than nothing.
func canAsk(s *tui.Session) bool { return s.Ready() == nil }
