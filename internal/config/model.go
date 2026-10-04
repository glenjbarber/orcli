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
// # What it does to last_model
//
// Choosing a model records the one it replaced under `last_model`, so `/model last`
// can go back to it. The recording is a part of choosing rather than a separate
// step, since a reader who typed a model and then typed `/model last` is asking to
// come back to where they were, and that only works if the model they left is the
// one that was written down when they left it.
//
// A model chosen with `last` recorded is the previous model on the way back, not
// the one it was swapped with, so two `last` commands in a row put the reader where
// they started. That is the swap property and it is what makes the command safe to
// press twice.
//
// # What this does not do
//
// It does not check the value against the endpoint's catalogue. There is no
// catalogue in the client: internal/openrouter carries the wire types and the
// request path, and no model list. A reader who names a model the endpoint does not
// offer finds out when a request is refused, which is a worse place to find out than
// here. The check belongs to whoever fetches the catalogue, and ModelIsOffered is the
// shape of it, so a caller is handed a check rather than being told one was done.
//
// The edit copies the file as bytes rather than re-encoding it, for the reason
// WriteColor gives, and it refuses a file at any other mode, as every writer here
// does.
func WriteModel(path, model string) error {
	return writeModelPair(path, model, "")
}

// WriteModelSwap sets the model and records the one it replaces.
//
// It is a separate writer rather than a flag on WriteModel, since the two are not
// the same operation. WriteModel is what the confirmation does: a turn proved the
// model works and the file should say so, and the model it replaced was one the
// reader chose by hand and has not moved away from. A swap is what `/model last`
// does, and it moves both members at once, so a writer that did only one of them
// would leave a file whose two members disagree about which is current.
func WriteModelSwap(path, model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("config: no model was given")
	}

	current, err := readModelMember(path)
	if err != nil {
		return err
	}

	return writeModelPair(path, model, current)
}

// writeModelPair sets the model and the one it replaced, in one pass.
//
// Two setMember calls would be two reads of the file and two writes, and a reader
// whose terminal died between them would be left with a model and no last_model or a
// last_model and no model. One read and one write is the shape of the thing: the two
// members are one decision.
func writeModelPair(path, model, last string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("config: no model was given")
	}

	data, err := readForEdit(path)
	if err != nil {
		return err
	}

	rendered := quoteJSONString(model)
	edited, err := setMember(data, modelKey, rendered)
	if err != nil {
		return err
	}
	if err := checkMember(edited, modelKey, rendered); err != nil {
		return fmt.Errorf("config: the edit did not verify: %w", err)
	}

	// A swap records the model being left, and a plain write records nothing. An
	// empty last_model is written as an empty string rather than skipped, since a
	// file carrying a last_model the reader cannot read back is worse than one
	// carrying none, and a swap always has a model to record.
	prev := quoteJSONString(strings.TrimSpace(last))
	if last != "" {
		edited, err = setMember(edited, lastModelKey, prev)
		if err != nil {
			return err
		}
		if err := checkMember(edited, lastModelKey, prev); err != nil {
			return fmt.Errorf("config: the edit did not verify: %w", err)
		}
	}

	return osWriteFile(path, edited)
}

// lastModelKey is the member the model a reader left lives under.
//
// It is named once so a writer and a reader cannot disagree about the spelling, the
// reason modelKey is named. The spelling carries no version prefix, since a reader
// editing their own configuration should not have to know which build wrote a member
// to leave it alone.
const lastModelKey = "last_model"

// readModelMember reads the model currently in the file.
//
// It is a read rather than a field on Config, since the writer is handed a path and
// not a configuration, and a writer that took a Config would be a caller holding two
// things that can disagree about which model is current.
func readModelMember(path string) (string, error) {
	data, err := readForEdit(path)
	if err != nil {
		return "", err
	}

	span, found := findMember(data, modelKey)
	if !found {
		return "", nil
	}

	var model string
	if err := json.Unmarshal(data[span.start:span.end], &model); err != nil {
		return "", fmt.Errorf("config: the %s member is not a string: %w", modelKey, err)
	}
	return model, nil
}

// ReadModelPair returns the model and the one a reader left, as the file has them.
//
// It is exported because a caller swapping the pair needs to know what it is
// swapping from and to, and a caller that re-read the file through Load would be
// doing the read twice with a different set of members in between.
func ReadModelPair(path string) (model, last string, err error) {
	data, err := readForEdit(path)
	if err != nil {
		return "", "", err
	}

	for _, key := range []string{modelKey, lastModelKey} {
		span, found := findMember(data, key)
		if !found {
			continue
		}
		var value string
		if err := json.Unmarshal(data[span.start:span.end], &value); err != nil {
			return "", "", fmt.Errorf("config: the %s member is not a string: %w", key, err)
		}
		switch key {
		case modelKey:
			model = value
		case lastModelKey:
			last = value
		}
	}
	return model, last, nil
}

// readForEdit reads the bytes a writer is about to edit.
//
// The mode and the object checks are here rather than in each writer, since every
// writer has the same three steps before it touches a byte and a writer that
// skipped one would write into a file another account can read.
func readForEdit(path string) ([]byte, error) {
	if err := checkMode(path); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if !isObject(data) {
		return nil, fmt.Errorf("config: %s: %w", path, ErrNotAnObject)
	}
	return data, nil
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
	data, err := readForEdit(path)
	if err != nil {
		return err
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
