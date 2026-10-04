# /cloudflare Design Document

## 1. Overview
The `/cloudflare` command in the `orcli` interface lets the reader manage Cloudflare
resources directly. The client performs the API calls itself; the model is not involved
and does not need to be.

This is a slash command in the interface, not a `cmd/orcli` subcommand and not a tool the
model calls. It belongs in `internal/tui` beside `/alias`, `/attribute` and `/connect`,
with an entry in the command table carrying `Name: "cloudflare"` and an `Args`
description for the completer.

```
/cloudflare <subcommand> [args]
```

## 2. Credential handling
- The Cloudflare API key is read from the project configuration file, in a nested provider
  object rather than a flat top-level key, so further providers can be added under the
  same block without changing the shape of the file.
- The key is never read from the environment. An environment variable override is not
  supported and must not be added, on the same terms as the OpenRouter API key.
- The reader loads the key at start-up and passes it to every API call.
- The key is never written to logs, never bound for a log row, and never committed to
  version control; the configuration file is listed in `.gitignore`.
- The reader supplies the key by editing the configuration file. There is no `--api-key`
  flag, since a credential typed at a prompt is bound for a request that is never
  filtered for it.
- The absent Cloudflare key is not fatal. Only the OpenRouter key is an absence that is
  fatal; a missing provider block leaves the command refusing with a message naming what
  is missing and where to put it.

## 3. Configuration shape

```json
{
  "api_key": "sk-or-v1-...",
  "cloudflare": {
    "api_key": "ABC123.XYZ789"
  }
}
```

The nested block is optional and is absent on every reader who does not use it. A file
written by a newer client must still open in an older one, so an unknown or unread block
is skipped rather than refused, on the same terms as an unknown top-level key.

## 4. Sub-commands

| Sub-command | Description | Example arguments |
|-------------|-------------|-------------------|
| `dns` | Manage DNS records. | `list`, `add`, `delete`, `edit` |
| `zone` | Operate on zones. | `list`, `info` |
| `cache` | Cache purges and settings. | `purge`, `status` |
| `page-rules` | Page-rule creation and deletion. | `create`, `delete` |
| `account` | Account-level info. | `info` |

```
/cloudflare dns list
/cloudflare dns add --name <string> --type <A|AAAA|CNAME|...> --content <string> [--ttl <seconds>]
/cloudflare dns delete --record-id <uuid>
/cloudflare zone list
/cloudflare zone info --zone-id <uuid>
/cloudflare cache purge --url <string>
/cloudflare cache purge --pattern <string>
/cloudflare page-rules create --target <string> --actions <json>
/cloudflare page-rules delete --rule-id <uuid>
/cloudflare account info
```

## 5. Write confirmation

A sub-command that changes state at a third party does not write immediately. It fetches
the current state, shows the reader what the change would be, and holds the change until
it is confirmed.

### 5.1 Where a diff is possible

Where the API can be read before the write, the confirmation is a **unified diff of the
actual prior state against the proposed state**, syntax-highlighted:

```
--- zone: example.com   record: app.example.com
+++ zone: example.com   record: app.example.com
@@ -1,4 +1,4 @@
   type: A
-  content: 198.51.100.7
+  content: 203.0.113.42
   ttl: 300
   proxied: false
```

A creation has no prior state to diff against, and renders as a whole-record addition from
nothing. A deletion renders as the removal of the record as it currently stands.

### 5.2 Where a diff is not possible

Some resources have no readable prior state. `cache purge` has nothing to fetch that would
show what purging a URL changes, and `page-rules create` has a list of existing rules
rather than a before and after of the one being created. For these, the confirmation is a
**plain human-readable summary of the request**, with nothing fetched:

```
about to purge https://example.com/path from the cache for example.com

  /cloudflare confirm    apply this
  anything else          cancel
```

### 5.3 Confirmation

Confirmation is a second command rather than a keypress:

```
/cloudflare confirm
```

`/cloudflare confirm` applies exactly the change that was shown, which is why the proposed
change is held in the session between the two commands rather than rebuilt from the
arguments typed a second time. The proposal is consumed on confirmation, refused on
anything else, and is held for one command only: a second `/cloudflare` call before the
confirmation replaces it.

The keypress form is the better interface and is not available yet, since `internal/tui`
has no input path; the command form works today because the command table and the
completer already exist.

### 5.4 Reads

Reads are not confirmed. `dns list`, `zone list`, `zone info`, `cache status` and
`account info` run directly and report their result.

## 6. Output format
- API output is JSON. It is pretty-printed for reading by default.
- If `--json` is passed, the raw JSON is emitted unchanged.
- Confirmations render as the unified diff or the summary above, not as JSON.
- The command supports a `--verbose` flag, reported in the log.

## 7. Error handling
- All HTTP errors from the Cloudflare API are captured and reported as a result carrying
  the error, on the same terms as every other call this client makes.
- Error messages are written to stderr and displayed to the user.
- The exit status is non-zero on failure; a short, plain-text summary precedes the JSON
  payload when available.
- Example error output:
  ```
  Error 400: validation failure
  {
    "errors": [{"message": "invalid DNS name"}],
    "success": false
  }
  ```
- A refused or failed confirmation is reported as `pending`, since the work is finished
  and what is outstanding is a resolution rather than more writing.

## 8. Package layout

`internal/tui` reaches the API through a new leaf package beside `internal/openrouter`,
since `AGENTS.md` holds that the packages are leaves and import one another in no
direction. It is named `internal/cloudflare`, and it carries:

- the client, the credential filter, and the five sub-command surfaces,
- the credential filter applied to every string bound for a log row or an error string,
- the current-state read each write confirms against,
- and the tests.

`internal/tui` must never reach `internal/tools`, which is the direction `AGENTS.md` holds
must not appear. Reaching `internal/cloudflare` from `internal/tui` is a new edge and needs
a reason, which is the one above: the command is in the interface, so the client is reached
from there and not from `main`.

## 9. Design constraints from `.orcli.md`
- **Writing style** - American English, direct prose, no contractions, no em dashes,
  straight double quotes and straight apostrophes only.
- **Command style** - BSD style, for example `sed -i ''`, not GNU.
- **Commit messages** - third person, passive voice, subject near 72 characters, trailer
  `Co-Authored-By: COMMIT_ATTRIBUTION`.
- **File formatting** - this markdown adheres to the same straight-quote rules and uses no
  non-ASCII characters.

## 10. Quick-start example
```
# Store your key by editing the configuration file:
# {
#   "api_key": "sk-or-v1-...",
#   "cloudflare": {
#     "api_key": "ABC123.XYZ789"
#   }
# }

# List every zone:
/cloudflare zone list

# Propose an A record, shown as a diff against the current record:
/cloudflare dns add --name app.example.com --type A --content 203.0.113.42 --ttl 300

# Apply exactly what was shown:
/cloudflare confirm
```