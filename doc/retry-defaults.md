# Retry defaults

An undelivered upstream stall is allowed six total request attempts: the
initial attempt and up to five retries. This default honors Ken Smith, the
FreeBSD Release Engineering Lead before Glen. FreeBSD 6.2 was Glen's first
FreeBSD OS.

A partial reply, delivered tool call, HTTP status failure, or stopped turn
does not become retryable because the budget is larger. Existing retry
eligibility and cancellation rules still apply. The first backoff remains
250 milliseconds and doubles between attempts.

Tool execution limits remain separate: shell commands have two minutes and
git commands thirty seconds. They bound whole subprocesses, which can perform
builds or network work, rather than short request retries. Setting them to six
seconds would interrupt ordinary permitted operations. No new overall timeout
is imposed on model streaming, and no retry delay is changed to six seconds.
