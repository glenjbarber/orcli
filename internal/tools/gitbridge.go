package tools

import "fmt"

// gitThroughShell checks a git call made through the shell tool.
//
// git appears in shellPermitted, so a model can run it there. Without this the
// subcommand is never checked against gitPermitted, and every refusal the git tool
// makes, including the nine subcommands that remove something the repository cannot
// recover, is reachable by asking the shell instead. A model that read one refusal
// message would find the other route open.
//
// The checks are the ones the git tool makes: the subcommand against the lists, and the
// options that write a file, run a program the repository names, or redirect where git
// reads at all. Nothing else is repeated, since the paths are already checked by the
// shell's own argument check and the containment is the same working directory.
func gitThroughShell(args []string) error {
	if len(args) == 0 {
		return nil
	}

	sub := args[0]

	// An option before the subcommand is a git option, not a subcommand: git -C
	// somewhere status is a command with a global option in front of it. Those are
	// refused below as options, and there is no subcommand to judge here.
	if isOption(sub) {
		return nil
	}

	if reason, refused := gitRefusedSubcommands[sub]; refused {
		return fmt.Errorf("%w: git %s is not run by this tool, because %s",
			ErrRefusedGit, sub, reason)
	}
	if !gitPermittedMap[sub] {
		return fmt.Errorf("%w: git %s is not permitted through the shell either; "+
			"this tool runs %s", ErrRefusedGit, sub, permittedSubcommands())
	}

	for _, arg := range args[1:] {
		if reason, refused := refusedGitOption(sub, arg); refused {
			return fmt.Errorf("%w: git %s may not be given %s: %s",
				ErrRefusedGit, sub, arg, reason)
		}
	}
	return nil
}

// gitSubcommandDescription names the rule to a model, so a refusal is not a dead end.
//
// git is on the shell list, so the subcommand is checked against the git allowlist
// rather than being reachable only through the git tool.
func gitSubcommandDescription() string {
	return "git may only be used for: " + permittedSubcommands() + ". A git subcommand " +
		"that removes work or rewrites history is refused whether it is asked for here " +
		"or through the git tool."
}
