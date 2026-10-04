package tui

// cloudflareCommand is the `/cloudflare` entry in the table.
//
// It is here rather than only in the dispatcher in cmd/orcli for the reason the
// table exists at all: a command the dispatcher runs and the table does not name is a
// command a reader cannot find. They can type it and it works, and nothing tells them
// it is there. `/help` does not list it and tab does not complete it, and the fact
// that the one API this program speaks to is missing from the list of what it can do
// is the part a reader notices.
//
// The Args line names the sub-commands rather than a grammar, since the table carries
// descriptions and not argument parsing, and the parsing lives where the command is
// implemented.
var cloudflareCommand = Command{
	Name:    "cloudflare",
	Args:    "SUBCOMMAND ARGS",
	Summary: "manage Cloudflare DNS records, showing each change before applying it",
}

// CloudflareCommand returns the table entry for /cloudflare.
//
// It is exported for the same reason AttributeCommand is: the dispatcher in main
// builds its own table and needs to know the spelling a reader types, so it asks here
// rather than repeating the name in two places that can drift.
func CloudflareCommand() Command { return cloudflareCommand }
