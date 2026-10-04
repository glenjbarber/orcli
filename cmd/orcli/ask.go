package main

import (
	"context"
	"strings"

	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tui"
)

// ask sends one question to the model and writes what comes back into the session.
//
// It is the whole of the turn wiring, and it is here rather than in internal/tui for the
// reason the dispatcher is: internal/tui holds no credential and reaches for no client,
// so something outside it has to hold the transport and the session together. This is
// that something, and it is the only place the two meet.
//
// The reply is held for the turn and written whole, which is the decision the
// interface was redesigned for. A log that grows a row per token is a log that
// scrolls past what the reader was reading, and a reply arriving as one row is what
// the frame was built to show.
//
// # A turn is the connection probe
//
// The reader's own first question is the probe. There is no `/connect` command and
// no separate check: a turn that comes back with text has proved the credential and
// the model together, and that is what confirms the model was good before anything
// is written to the configuration file.
//
// A turn that produced nothing, however it ended, proves nothing and writes nothing.
// `confirmed` is reported to the caller rather than acted on here, since the write is
// a named writer in internal/config and this function does not know the path.
//
// A turn carries the question and nothing else. There is no history in the request
// and no tools are offered, which is the one place the interface is not yet a chat
// client rather than a question sender. It is left that way on purpose rather than
// guessed at.
func ask(s *tui.Session, c *openrouter.Client, attribution string, confirmed func(model string)) tui.AskFunc {
	return func(ctx context.Context, question string, level int) error {
		turnCtx, err := s.Begin(ctx, question, level)
		if err != nil {
			return err
		}

		model := s.Options().Model

		var reply strings.Builder
		c.Chat(turnCtx, openrouter.Request{
			Model: model,
			Messages: []openrouter.Message{
				{Role: "user", Content: question},
			},
			Stream:        true,
			AttributionID: attribution,
		}, func(e openrouter.Event) {
			switch e.Kind {
			case openrouter.EventDelta:
				reply.WriteString(e.Text)

			case openrouter.EventError:
				// The text that arrived before a failure is kept and the failure is
				// reported beside it, since the text is usually more useful than
				// the error alone and a reader handed only an error has nothing to
				// read.
				s.Deliver(reply.String(), level)
				if e.Err != nil {
					s.Notice(e.Err.Error(), level, tui.RoleFailure)
				}

			case openrouter.EventFinish:
				// Deliver is a no-op for an empty reply, so a turn that failed
				// before any text does not write a blank row.
				s.Deliver(reply.String(), level)
				s.Finished(e.Reason)

				// A reply with text in it is the confirmation, and an empty one is
				// a refusal even when the endpoint called the turn finished. A
				// stream that ended without delivering anything has not proved the
				// credential works, and writing the model on that evidence would
				// record a model nobody has reached the endpoint with.
				if confirmed != nil && reply.Len() > 0 && e.Finished {
					confirmed(model)
				}
			}
		})
		return nil
	}
}

// canAsk reports whether a model can be asked anything at all.
//
// It is the dispatcher's answer to a question it needs before it can choose between
// sending its guidance to the model and falling back to its own text: a reader with
// no credential and no model chosen cannot be answered by a model, and /cloudflare
// must still produce a result rather than nothing.
func canAsk(s *tui.Session) bool { return s.Ready() == nil }
