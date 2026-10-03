package tools

import "os/exec"

// lookPath finds a program on PATH.
//
// It is a field rather than a direct call so a test can point the shell tool at a
// directory of stand-in programs instead of at whatever happens to be installed on
// the machine running the suite. The shipped behavior is exec.LookPath, which is
// what a reader's machine has.
var lookPath = exec.LookPath

// runProgram runs a resolved program in dir with an argument array.
//
// exec.Command is given the program as a resolved path and the arguments as separate
// entries, so nothing inside an argument is ever interpreted: a pipe in an argument
// is a character in a string and not an operator. The environment is set explicitly
// rather than inherited, since a subprocess that inherits the reader's environment
// inherits whatever secrets happen to be in it.
func runProgram(path string, args []string, dir string) ([]byte, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = environment()
	return cmd.Output()
}
