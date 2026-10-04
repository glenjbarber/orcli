package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// WriteModel sets the model identifier a session answers with.
//
// It is the fourth writer, and the reason it is a writer here rather than a
// function writing bytes in the command that asked is the reason the other three
// are writers: the file holds a credential, and a writer outside this package is a
// second thing that can corrupt it.
//
// The value is the model identifier the endpoint offers, such as
// `stealth/space-bunny-alpha`. It is not a friendly name the reader types, because
// a value that reaches a commit trailer is either a name the endpoint uses or
// something a reader invented, and the invented one is wrong in six months.
//
// What this does not do is check the value against the endpoint's catalogue. There
// is no catalogue in the client: internal/openrouter carries the wire types and the
// request path, and no model list. A reader who names a model the endpoint does not
// offer finds out when a request is refused, which is a worse place to find out than
// here. The check belongs to whoever fetches the catalogue, and ModelIsOffered is
// the shape of it, so a caller is handed a check rather than being told one was
// done.
//
// The edit copies the file as bytes rather than re-encoding it, for the reason
// WriteColor gives, and it refuses a file at any other mode, as every writer here
// does.
func WriteModel(path, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("config: no model was given")
	}
	return setTopLevel(path, modelKey, quoteJSONString(model))
}

// modelKey is the member the model identifier lives under.
//
// It is named once so a writer and a reader cannot disagree about it, and the
// spelling is the one Config carries as its field, so a file this writes is a file
// Load reads.
const modelKey = "model"

// setTopLevel sets one top-level member of the configuration to a rendered value.
//
// It is the one operation WriteColor and WriteModel share, and it is a function
// rather than a pair of near-copies because the byte-level care is the whole
// difficulty: a writer that decoded the file and re-encoded it would reorder keys,
// reindent, and normalise line endings, and the file holds a credential.
//
// The value arrives already rendered, so a caller writing a string quotes it and a
// caller writing an array renders it. Taking a rendered value rather than a Go one
// keeps the quoting in one place per value type instead of spreading it through
// every writer that happens to write a string.
func setTopLevel(path, key, value string) error {
	if err := checkMode(path); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if !isObject(data) {
		return fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}

	edited, err := setMember(data, key, value)
	if err != nil {
		return err
	}
	if err := checkMember(edited, key, value); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}
	return osWriteFile(path, edited)
}

// quoteJSONString renders a Go string as a JSON string.
//
// A model identifier is a path with a slash in it, and a reader may paste one
// carrying a backslash, a quote, or a character outside ASCII. Quoting through
// json.Marshal rather than by hand is the whole reason this is a function: a
// hand-quoted path is the one place in this package that could write a value that
// does not parse, and a member that does not parse is a file no reader can open.
func quoteJSONString(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		// A string cannot fail to marshal. Writing the empty string rather than a
		// partial quote keeps the file parseable, and the check in the caller
		// catches it as a mismatch rather than as a corrupt file.
		return `""`
	}
	return string(quoted)
}

// ModelIsOffered reports whether model is in the catalogue the endpoint returned.
//
// It is here rather than in the command that asks because the question needs the
// catalogue and the catalogue is fetched once, so a caller holds the list and hands
// it in rather than three commands each fetching their own. It is exported for that
// reason: the alternative is a check that exists three times.
//
// An empty catalogue is a refusal rather than a match for nothing. A reader who
// named a model with no list to check it against has told us nothing about whether
// it exists, and treating the absence as a mismatch would refuse every model rather
// than none, which reads as the endpoint offering none rather than the client not
// having asked.
func ModelIsOffered(catalogue []string, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("config: no model was given")
	}
	if len(catalogue) == 0 {
		return ErrNoModelList
	}
	for _, offered := range catalogue {
		if offered == model {
			return nil
		}
	}
	return fmt.Errorf("config: %s is not one of the %d models the endpoint offers",
		model, len(catalogue))
}
