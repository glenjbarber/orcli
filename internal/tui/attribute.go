package tui

// attributeCommand is the `/attribute` entry in the table.
//
// It is a separate entry rather than an alias of `/model` for one reason: the two
// are not the same command today. `/model` shows or chooses, and it is the reader's
// everyday word for it. `/attribute` names the model in the sense that a commit
// carries a model rather than a person, and it is the entry that says the value has
// to be one the endpoint offers. A reader who typed the wrong model once and
// watched a request be refused would rather be told before the request.
//
// The alias machinery can bind one name to the other, and `/alias` does that for a
// reader who wants it. The two being the same command is a reader's decision rather
// than this table's, and a table that silently merged them would take it for them.
var attributeCommand = Command{
	Name:    "attribute",
	Args:    "NAME",
	Summary: "set the model this session answers with, from the ones offered",
	Aliases: []string{"attribution"},
}

// AttributeCommand returns the `/attribute` entry.
//
// It is a function rather than a field read from the table because the command is
// built in this file and registered in command.go's init. Reaching for it through
// Lookup would work, since the entry is in the table, and this says which value is
// meant without making a caller depend on a table a reader can be looking at.
func AttributeCommand() Command { return attributeCommand }
