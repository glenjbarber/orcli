package config

import "errors"

// ErrNoAPIKey is returned by Load when the file parsed but carried no
// credential.
//
// It is not a fatal error. The caller stands in a configuration so the
// interface still opens and reports the absence in the pane, because an
// interface that refuses to open leaves nothing on screen explaining why.
//
// It is distinct from a missing file and from a malformed one, since those are
// different faults with different remedies: a missing file is first-time setup,
// a malformed one is a file a reader has to fix.
var ErrNoAPIKey = errors.New("config: no API key in the configuration file")

// ErrNotFound is returned when no configuration file exists at any searched
// path.
//
// It is separate from a read failure on purpose. A file that exists and cannot
// be read is a fault, and it is fatal; no file at all is a state, and the
// remedy is to write one.
var ErrNotFound = errors.New("config: no configuration file")

// ErrBadMode is returned when the file exists and is not `0600`.
//
// This is fatal rather than a warning. The file holds a credential, and a mode
// that permits another account to read it has already leaked it by the time
// this is reported.
var ErrBadMode = errors.New("config: the configuration file must be mode 0600")

// ErrNotAnObject is returned when the file parses but is not a JSON object.
//
// An array or a bare value at the top level parses, which is the trap here: a
// reader would be told the file is valid while nothing in it could be read.
var ErrNotAnObject = errors.New("config: the configuration file is not a JSON object")

// ErrNoModelList is returned when a model cannot be checked because the
// endpoint's catalogue has not been fetched.
//
// It is a named error rather than a bare refusal so a caller can tell "this
// model does not exist" from "we never asked". The two need different
// remedies: one is a reader who typed it wrong, and the other is a fetch that
// failed.
var ErrNoModelList = errors.New("config: the model catalogue has not been fetched")
