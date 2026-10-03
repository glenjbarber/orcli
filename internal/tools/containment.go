package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// containment resolves a path a subprocess tool was given and reports whether
// it stays inside the working directory.
//
// This is the only defence git and shell have, and it is a weaker one than the
// open descriptor the file tools use. A descriptor cannot be argued with: the
// kernel resolves every path beneath it and there is nowhere above it to go. A
// subprocess is given a working directory and a list of arguments, and it is
// free to read whatever it likes by other means. So this is a string comparison
// against the resolved working directory, and it is the reason the filesystem
// tools use a descriptor instead.
//
// It is still worth having, and it is checked before the process is started
// rather than after. Three things it catches that a model gets wrong by habit:
//
//   - "..", and any path that walks upward through it, which is the ordinary
//     way a model asks for a file it should not have;
//   - an absolute path, which would otherwise name a file anywhere on the host
//     regardless of the working directory it was given;
//   - a symlink whose target is outside, which a cleaned path cannot see and
//     which is the same trick the descriptor exists to defeat.
//
// A path that does not exist yet is resolved as far as it can be and then
// judged on the cleaned form. A tool that refused to create a file because the
// directory holding it does not exist yet would refuse most of what it is for.
func containment(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: no path was given", ErrOutsideRoot)
	}

	// An absolute path is refused rather than made relative. Stripping the
	// leading separator would turn /etc/passwd into etc/passwd under the root,
	// which is a different file from the one asked for and a confusing way to
	// say no.
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s is an absolute path", ErrOutsideRoot, path)
	}

	// The root is resolved as well as the candidate. On macOS a temporary
	// directory arrives as /var/folders/... and is a symlink to
	// /private/var/folders/..., so comparing a resolved candidate against an
	// unresolved root refuses every legitimate path under it. Both sides are
	// put in the same form before they are compared, which is the whole of what
	// makes the comparison mean anything.
	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return "", err
	}

	abs := filepath.Join(resolvedRoot, path)
	cleaned := filepath.Clean(abs)

	// The containment test is on the cleaned absolute path rather than on the
	// path as written, since "../x" and "a/../../x" are the same request.
	//
	// The prefix test needs the separator. filepath.Rel is not used because a
	// relative path that begins with ".." is exactly the case being refused,
	// and reading it as a prefix test would pass "/work/a..b" for "/work/a".
	if !within(resolvedRoot, cleaned) {
		return "", fmt.Errorf("%w: %s", ErrOutsideRoot, path)
	}

	// A symlink pointing out of the tree is resolved where it exists.
	// EvalSymlinks fails on a path that is not there yet, which is not a
	// refusal: a file being created has no target to inspect, and the cleaned
	// form was already judged above.
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		if !within(resolvedRoot, filepath.Clean(resolved)) {
			return "", fmt.Errorf("%w: %s resolves outside the working directory",
				ErrOutsideRoot, path)
		}
		return resolved, nil
	}

	return cleaned, nil
}

// resolveRoot returns the working directory in the form candidates are compared
// against.
//
// A root that does not resolve is reported rather than used unresolved, since a
// comparison against a root in one form and candidates in another refuses every
// path under it, which looks like a containment failure rather than a wrong
// path.
func resolveRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve the working directory %s: %v", ErrOutsideRoot, root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%w: resolve the working directory %s: %v", ErrOutsideRoot, root, err)
	}
	return filepath.Clean(resolved), nil
}

// within reports whether an absolute cleaned path is root or is beneath it.
func within(root, path string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// environment returns the environment a subprocess is given.
//
// It is PATH and HOME and nothing else. A subprocess that inherits the reader's
// environment inherits whatever secrets happen to be in it, and a tool that acts
// on a model's request has no business handing a model a credential the model
// did not ask for. HOME is included because a program with no HOME at all fails
// in ways that look like the containment is broken, and because a program
// writing a temporary file needs somewhere to put it.
//
// This is also the whole of the environment this package reads. PATH is read
// here and nowhere else.
func environment() []string {
	var env []string
	if path, ok := os.LookupEnv("PATH"); ok {
		env = append(env, "PATH="+path)
	}
	if home, ok := os.LookupEnv("HOME"); ok {
		env = append(env, "HOME="+home)
	}
	return env
}

// resolveProgram finds an allowed program through PATH.
//
// The name is resolved by bare name and never as a path, so a model asking for
// /bin/sh is asking for something outside the allowlist and is refused. The
// allowlist is a list of program names, and a name that resolves to a program
// outside it is a name that was not on the list.
func resolveProgram(name string, allowed []string) (string, error) {
	if !contains(allowed, name) {
		return "", fmt.Errorf("tools: %s is not an allowed program; the allowed programs are %s",
			name, strings.Join(allowed, ", "))
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("tools: %s names a path, and a program is named by bare name", name)
	}

	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("tools: %s was not found on PATH: %w", name, err)
	}
	return path, nil
}

// contains reports whether name is in the list.
func contains(list []string, name string) bool {
	for _, entry := range list {
		if entry == name {
			return true
		}
	}
	return false
}
