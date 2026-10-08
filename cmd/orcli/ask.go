package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glenjbarber/orcli/internal/apiary"
	"github.com/glenjbarber/orcli/internal/notion"
	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tools"
	"github.com/glenjbarber/orcli/internal/tui"
)

// maxToolRounds bounds how many times one turn can call a tool before this gives up
// and reports the turn stopped rather than looping forever against a model that keeps
// asking for more. The large ceiling permits long agent workflows while still making
// a runaway turn finite.
const maxToolRounds = 4096

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
// # Two system messages, not one
//
// adr-0000042's capability message - orcli naming itself and what this session
// actually has, assembled from Session.Options()/Ready() and the Cloudflare check -
// is sent first, built fresh every turn by capabilities. If AGENTS.md also exists in
// the session's working directory, its contents follow as a second system message,
// naming it as the reader's own introduction rather than folding it into the first.
// The two are kept separate because they come from different places and change for
// different reasons: one reflects this session's live configuration, the other is
// whatever the reader put in a file.
func ask(s *tui.Session, c chatClient, attribution string, confirmed func(model string, silent bool), cloudflareReady func() bool, capture *debugLog, notionToken string, apiarySettings ...string) tui.AskFunc {
	toolset := newToolset(s.Options().WorkingDir, notionToken, apiarySettings...)
	schemas := toolSchemas(toolset)
	intro := introduction(s.Options().WorkingDir)
	docs := documentation(s.Options().WorkingDir)

	return func(ctx context.Context, question string, level int, silent bool) error {
		begin := s.Begin
		deliver := s.Deliver
		notice := s.Notice
		finishedFn := s.Finished
		if silent {
			// The HELO is the only caller that passes silent: its text instructs the
			// model, it is not something the reader typed, and BeginSilent is the
			// primitive that sends it to the model without writing it into the log as
			// a question row. See BeginSilent's own doc comment for why.
			//
			// The reply is not silenced the same way. A prior version of this code
			// swapped Deliver, Notice and Finished to their *Silent counterparts too,
			// on the theory that the model's own answer to a question the reader
			// never asked should not surface as a row - but that directly contradicts
			// maybeSendHELO's own doc comment, which promises the greeting "lands in
			// the log, the same way any other turn's reply does," and Glen confirmed
			// (2026-10-07) that the visible greeting is what he actually wants: a
			// HELO whose reply nobody ever sees is a HELO that might as well not have
			// run. Only begin stays silent; the question text is synthetic and still
			// should not show as a fake question row, but the answer is real.
			begin = s.BeginSilent
		}
		turnCtx, err := begin(ctx, question, level)
		if err != nil {
			return err
		}

		// The model is read through the session rather than off the options value,
		// since `/model` can change it while a turn is in flight and the turn has to
		// be sent with the model in force when it was asked rather than the one the
		// file happened to hold when the request was built.
		model := s.Options().Model
		approval := string(s.Options().Approval)

		messages := make([]openrouter.Message, 0, 4)
		messages = append(messages, openrouter.Message{
			Role:    "system",
			Content: capabilities(s, toolset, cloudflareReady),
		})
		if intro != "" {
			messages = append(messages, openrouter.Message{Role: "system", Content: intro})
		}
		if docs != "" {
			messages = append(messages, openrouter.Message{Role: "system", Content: docs})
		}
		messages = append(messages, openrouter.Message{Role: "user", Content: question})
		for round := 0; round < maxToolRounds; round++ {
			var reply strings.Builder
			var calls []openrouter.ToolCall
			var reason string
			var finished bool
			var failed error
			capture.record("request", map[string]any{"model": model, "messages": messages, "tools": schemas})

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
					capture.record("assistant_delta", e.Text)

				case openrouter.EventTool:
					if e.ToolCall != nil {
						calls = append(calls, *e.ToolCall)
						capture.record("tool_call", *e.ToolCall)
					}

				case openrouter.EventUsage:
					capture.record("usage", e.Usage)
					if e.Usage != nil {
						input, output := 0, 0
						hasInput, hasOutput := e.Usage.PromptTokens != nil, e.Usage.CompletionTokens != nil
						if hasInput {
							input = *e.Usage.PromptTokens
						}
						if hasOutput {
							output = *e.Usage.CompletionTokens
						}
						cost, hasCost := 0.0, false
						if e.Usage.CostUSD != nil {
							cost, hasCost = *e.Usage.CostUSD, true
						} else if e.Usage.Cost != nil {
							cost, hasCost = *e.Usage.Cost, true
						}
						s.AddUsage(input, output, cost, hasInput, hasOutput, hasCost)
					}

				case openrouter.EventDone:
					capture.record("stream_done", nil)

				case openrouter.EventError:
					// The text that arrived before a failure is kept and the
					// failure is reported beside it, since the text is usually
					// more useful than the error alone and a reader handed only
					// an error has nothing to read.
					deliver(reply.String(), level)
					if e.Err != nil {
						capture.record("stream_error", e.Err.Error())
						notice(e.Err.Error(), level, tui.RoleFailure)
						failed = e.Err
					}

				case openrouter.EventFinish:
					reason = e.Reason
					finished = e.Finished
					capture.record("finish", map[string]any{"reason": reason, "finished": finished})
				}
			})
			if err != nil {
				capture.record("request_error", err.Error())
				notice(err.Error(), level, tui.RoleFailure)
				finishedFn("failed")
				return nil
			}
			if failed != nil {
				return nil
			}

			if len(calls) == 0 {
				// Deliver is a no-op for an empty reply, so a turn that failed
				// before any text does not write a blank row.
				deliver(reply.String(), level)
				finishedFn(reason)

				// A reply with text in it is the confirmation, and an empty one
				// is a refusal even when the endpoint called the turn finished.
				// A stream that ended without delivering anything has not
				// proved the credential works, and writing the model on that
				// evidence would record a model nobody has reached the
				// endpoint with.
				if confirmed != nil && reply.Len() > 0 && finished {
					confirmed(model, silent)
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
				result := runTool(toolset, approval, call)
				capture.record("tool_result", map[string]string{"call_id": call.ID, "name": call.Function.Name, "content": result})
				messages = append(messages, openrouter.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    result,
				})
			}
		}

		notice(fmt.Sprintf("stopped after %d rounds of tool calls", maxToolRounds), level, tui.RoleFailure)
		finishedFn("stopped")
		return nil
	}
}

// newToolset builds the tools a turn may call, contained to dir.
//
// Filesystem tools are omitted, not fatal, if the root cannot be opened - a session
// without them is a session with less in it rather than one that cannot run, the same
// rule NewFilesystem's own doc comment states for its caller.
func newToolset(dir, notionToken string, apiarySettings ...string) []tools.Tool {
	set := []tools.Tool{tools.NewGit(dir), tools.NewShell(dir)}
	if fs, err := tools.NewFilesystem(dir); err == nil {
		set = append(set, fs.Tools()...)
	}
	set = append(set, notion.New(notionToken).ToolSet()...)
	if len(apiarySettings) >= 2 && apiarySettings[0] != "" && apiarySettings[1] != "" {
		set = append(set, apiary.New(apiarySettings[0], apiarySettings[1]).ToolSet()...)
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
		case interface{ SetApproval(string) }:
			c.SetApproval(approval)
		}
		result := t.Run(json.RawMessage(call.Function.Arguments))
		if result.Err != nil {
			return result.Err.Error()
		}
		return result.Content
	}
	return fmt.Sprintf("tools: no tool named %q", call.Function.Name)
}

// capabilities builds adr-0000042's system message: orcli naming itself and what
// this session actually has right now, assembled from the same state the reader's
// own commands read rather than a second, hand-maintained list that could say
// something `/model` or `/cloudflare` has already made false.
//
// Absence is written out, not omitted - a model told only what exists cannot tell
// "not configured" from "not asked about yet," which is the position this record
// exists to keep it out of.
//
// Each tool's own Describe().Function.Description is reused verbatim rather than
// restated, for the same reason: a second copy of what a tool may do is a second
// copy that goes stale the day the tool's own list changes and this one does not.
func capabilities(s *tui.Session, toolset []tools.Tool, cloudflareReady func() bool) string {
	var b strings.Builder
	b.WriteString("You are talking to orcli, a terminal interface that sends your replies " +
		"straight to the reader's screen. This message names what this session actually " +
		"has configured right now; anything not named here is not available this turn.\n\n")

	if err := s.Ready(); err != nil {
		b.WriteString("Model: none configured. " + err.Error() + "\n")
	} else {
		opts := s.Options()
		fmt.Fprintf(&b, "Model: %s, via %s.\n", opts.Model, orNone(opts.Provider))
	}

	if len(toolset) == 0 {
		b.WriteString("Tools: none available this session.\n")
	} else {
		b.WriteString("Tools:\n")
		for _, t := range toolset {
			fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Describe().Function.Description)
		}
	}

	if cloudflareReady != nil && cloudflareReady() {
		b.WriteString("Cloudflare: a credential is configured; the connector is available.\n")
	} else {
		b.WriteString("Cloudflare: no credential is configured; the connector is not available.\n")
	}

	// There is no plugin system in this build: nothing in the tree registers a
	// plugin, enables one, or carries a list of them to read. This is named rather
	// than left out, on the same "absence is a statement" grounds as every other
	// line above, so a reader who asks what plugins are enabled is told none can be,
	// not left to guess whether the question was never asked.
	plugins := []string{"@notion", "@cloudflare"}
	for _, tool := range toolset {
		if strings.HasPrefix(tool.Describe().Function.Name, "apiary_") {
			plugins = append(plugins, "@apiary")
			break
		}
	}
	b.WriteString("Plugins: prefix a request with " + strings.Join(plugins, ", ") + " to explicitly request that plugin; structured subcommands are also available. Use @<plugin> help for capabilities and setup guidance.\n")

	// /level's active preset, when there is one, is written as its own sentence
	// rather than folded into the paragraph above: it is an instruction about how
	// to answer rather than a fact about what is configured, and the two should
	// not read as one kind of statement.
	if style := s.PresetStyle(); style != "" {
		b.WriteString("\n" + style + "\n")
	}

	return b.String()
}

// introduction reads AGENTS.md from dir, for the one system message a turn sends
// ahead of the question. AGENTS.md, not CONTEXT.md: it is the file a tree already
// names as read by default (see this repository's own AGENTS.md, "This file is read
// by default"), and the one goose and other tools already converge on, where
// CONTEXT.md was a project-specific pointer file with no standing outside Loreloom's
// own coordination documents and no claim on this role. A missing file is silence,
// not a failure: most working directories have none, and a session without one
// sends no introduction at all rather than an empty one.
func introduction(dir string) string {
	text, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		return ""
	}
	return string(text)
}

// heloQuestion is the question a session asks itself once, at the very start, so
// the reader's screen carries an introduction before they have typed anything.
// Glen named this exchange the HELO, canonically, after SMTP's own greeting
// command (2026-10-07).
//
// It is sent through the same ask closure as any question a reader types, which is
// what lets it see the same capability message, the same AGENTS.md introduction,
// and the same documentation listing every other turn sees - a second path that
// built its own greeting would be a second thing to keep in step with those three.
// The documentation listing reaching the model this way, unchanged, is what lets the
// greeting stay silent about doc/ and staged/ below: the names are already in its
// context the moment the reader does ask, so the HELO does not need to recite them
// first to make that true later.
const heloQuestion = "Briefly greet the user as orcli, grounded in the session capabilities above."

// documentation lists the names of orcli's own documentation under dir, so the
// capability message can point the model at doc/ and staged/ by name rather than
// have it guess at what detail exists or describe this build from training data
// rather than from the tree it is actually running in.
//
// Only names are listed, not contents: a reader who wants the detail behind a
// capability can read the file itself, through the filesystem tool when one is
// wired in, and a listing that inlined every file would be the AGENTS.md mistake
// repeated - a second copy of material that lives in one place already.
//
// Either directory missing is silence for that directory, on the same grounds as
// introduction's missing AGENTS.md: most working directories are not orcli's own
// checkout, and a session outside it has neither to list.
func documentation(dir string) string {
	var b strings.Builder
	list := func(heading, sub string) {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if len(names) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%s/):\n", heading, sub)
		for _, name := range names {
			fmt.Fprintf(&b, "- %s\n", name)
		}
	}

	list("Reference documentation", "doc")
	list("Design records and ADRs", "staged")

	if b.Len() == 0 {
		return ""
	}
	return "orcli's own documentation, for detail beyond this message:\n\n" + b.String()
}

// canAsk reports whether a model can be asked anything at all.
//
// It is the dispatcher's answer to a question it needs before it can choose between
// sending its guidance to the model and falling back to its own text: a reader with
// no credential and no model chosen cannot be answered by a model, and /cloudflare
// must still produce a result rather than nothing.
func canAsk(s *tui.Session) bool { return s.Ready() == nil }
