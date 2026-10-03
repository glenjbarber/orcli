// Package tools runs the calls a model makes on a reader's behalf.
//
// A call always produces a result. An unknown name, arguments that are not a
// JSON object, a body that returned an error, and a body that panicked are all
// reported as a result carrying that error, because a call producing no result
// at all leaves the turn waiting for something that never arrives, and a turn
// that waits is reported by a reader as a hang rather than as a fault.
//
// # Containment
//
// Filesystem containment is an open descriptor, not a prefix check. A prefix
// check on a cleaned path is defeated by exactly the symlink that a string
// comparison cannot see, so the root is opened with os.Root and every path is
// resolved through it. The root does not move for the session: there is no
// command that changes the working directory.
//
// Git and shell run subprocesses, so os.Root does not apply to them. They
// compare the resolved path against the resolved working directory and refuse a
// path that leaves the tree.
//
// # Never a shell
//
// Neither tool ever reaches a shell. Arguments go to the process as an array.
// A pipe, a redirect, and a chain are features of a shell, and offering them
// would mean running one, so the schema says so and a model that asks for a
// pipeline learns it is not available rather than having it silently split into
// arguments.
//
// # Environment
//
// PATH is the only environment variable this package reads, and it is read to
// resolve a program the shell tool was allowed to run. Every other variable is
// ignored, including OPENROUTER_API_KEY and any other name that looks like a
// credential or a setting. A subprocess is given an empty environment plus
// PATH, so a program this package runs cannot inherit a secret the reader did
// not intend to hand it.
package tools

import (
	"encoding/json"
	"fmt"
)

// Result is what a call produced.
//
// A result is always present, so a caller never has to ask whether one arrived.
// Content is what the tool returned, and is empty for a refusal, since a
// refusal has nothing to report beyond its reason.
type Result struct {
	// Content is what the tool produced, for a reader or a model.
	Content string

	// Err is why the call did not produce what it was asked for. It is nil on
	// a call that succeeded, and it is the reason for every refusal, including
	// an unknown name and a panic.
	Err error
}

// arguments is the decoded body of a call.
//
// It is decoded here rather than in each tool, so a body that is not an object
// is one error reported the same way everywhere instead of one per tool.
type arguments struct {
	// Path is the file or directory a filesystem call acts on.
	Path string `json:"path"`

	// Content is what a write puts in a file.
	Content string `json:"content"`
}

// Tool is one capability offered to a model.
//
// The set is fixed by this package and not by the configuration file. What a
// tool may do is decided here; what a reader is asked before it happens is
// decided by the interface package, and neither can widen the other.
type Tool interface {
	// Name is what a model calls it by.
	Name() string

	// Describe returns the schema offered to the model. It is the tool's own
	// description and it says plainly what the tool cannot do, since a model
	// told only what a tool can do will ask for the rest.
	Describe() Schema

	// Run performs the call and returns what happened.
	Run(args json.RawMessage) Result
}

// Schema is a tool declaration in the shape the endpoint expects.
type Schema struct {
	Type        string       `json:"type"`
	Function    FunctionSpec `json:"function"`
	Description string       `json:"description,omitempty"`
}

// FunctionSpec declares a tool's parameters.
type FunctionSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// invoke runs a tool and converts whatever happens into a Result.
//
// This is the whole of the guarantee, and it is deliberately a single function
// that every call passes through. A panic is recovered into an error, because a
// panic in a tool is a fault the reader can be shown and a fault that unwinds
// past the turn goroutine takes the session with it. A nil tool is reported
// rather than called, because a call that produces no result is the one outcome
// everything else here exists to prevent.
func invoke(t Tool, args json.RawMessage) (result Result) {
	if t == nil {
		return Result{Err: fmt.Errorf("tools: no tool to invoke")}
	}

	defer func() {
		if r := recover(); r != nil {
			result = Result{Err: fmt.Errorf("tools: %s panicked: %v", t.Name(), r)}
		}
	}()

	return t.Run(args)
}

// decode reads a call body as a JSON object.
//
// A body that is not an object is refused here rather than in each tool, so an
// arguments field that is a number or an array is one error with one shape
// instead of a decode failure per tool. An empty body is an object with no
// members, since a call with no arguments is a legitimate call.
func decode(args json.RawMessage) (arguments, error) {
	var a arguments

	if len(args) == 0 {
		return a, nil
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return a, fmt.Errorf("tools: the arguments are not a JSON object: %w", err)
	}
	return a, nil
}
