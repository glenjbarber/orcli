# GitHub and Notion task integration audit

## Scope and findings

The audit compared the existing task workflows with issue #53's requirements.

| Area | Before this change | Current task workflow |
| --- | --- | --- |
| GitHub credentials | `github.api_key` was parsed and preserved; it did not make requests | GitHub REST client uses only the configured token, login, and explicit repository allowlist |
| GitHub task operations | No GitHub tool or `@github` route | List assigned issues, fetch issue details, list comments, create/update issues, change state, and comment |
| Notion API | Generic REST tools already supported search, page/block/data-source operations, and comments | Adds an assignee-aware task tool alongside existing generic REST tools |
| Notion task scope | No task-specific source or assignee boundary | Task queries use one configured data source and assignee; individual pages are checked against that source |
| Permission handling | Existing tool approval and `OPENROUTER_TOOLS` policy | Task writes follow the same policy; deny mode blocks writes before the network request |
| Retry behavior | Not specified for task writes | No automatic write retries; transport/server failures identify an unknown outcome and tell the reader to inspect before retrying |
| Progress persistence | Conversation/session persistence and comments are separate features | Use `/save` to preserve the conversation and task context; post a task comment for a durable handoff visible in the task system |

Relevant code is in `internal/github`, `internal/notion/tasks.go`, task settings
in `internal/config`, tool registration in `cmd/orcli/ask.go`, and routing/help
in `cmd/orcli/dispatch.go` and `internal/tui/command.go`.

## Credentials and permissions

The GitHub credential is an Orcli-owned fine-grained personal access token in
the mode-0600 config file. Grant Issues read/write and Metadata read-only on
each selected repository. Set the account login and list every allowed
`owner/repo`; an omitted or empty allowlist enables no GitHub task tool. The
client will not address repositories outside that list. Do not paste the token
into a prompt or use another application's connector credential.

The Notion credential is an Orcli-owned integration token. Share the selected
data source with that integration and configure its ID, the assignee property
name, the assignee's Notion user ID, and the status property name. The tool
queries only that source, creates new tasks there assigned to that user, and
checks the parent source before operating on a specific task page. Existing
generic Notion tools remain available under the integration's Notion sharing
permissions.

Both tools use the established approval mode and `OPENROUTER_TOOLS` file rules.
Configure those rules before enabling writes if approval should be narrower.
Neither integration borrows credentials from the ChatGPT application.

## Verification and limits

Package-level HTTP tests use `httptest` to check request scope, assignee
filters, authorization headers, token redaction, deny mode, response handling,
and no-retry behavior. A live API run is intentionally not claimed: this
environment has no supplied Orcli GitHub/Notion credentials or designated safe
test repository/data source. Live verification requires those separately
provisioned resources. Never test writes against a user's task data merely to
prove credentials work.

Transport failure after a write begins cannot establish whether the remote
service applied the mutation. The tools surface that ambiguity and do not
retry. Inspect the issue/task and its comments before any manual retry.

## Progress and handoff

Use `/save NAME` to persist the current conversation and its collected context,
and `/load NAME` to resume it. For a handoff that must be visible to GitHub or
Notion collaborators, use the task comment operation with the blocker, current
status, next action, and evidence/check time. Orcli does not claim that an
unsaved in-memory session is durable or that a comment automatically updates
the Loreloom task ledger.
