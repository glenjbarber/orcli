# Loreloom shared policy

Read the Canonical Shared Policy: [ADR-0000000: Loreloom coordination policy](https://app.notion.com/p/3f0f5fa187af812b837eecf5b6d0cec5).
Use [ADR-0000007: Repository-scoped automatic ADR numbering](https://app.notion.com/p/3f1f5fa187af81dc840dd821e77e58a1) for the numbering and allocation workflow; it updates only those clauses of ADR-0000000.

**Current policy status (as of 2026-10-06)**
- Status: Accepted
- Approved by: Glen J Barber
- Approval date: 2026-10-06
- Supersedes: None
- Superseded by: ADR-0000007: Repository-scoped automatic ADR numbering (numbering/allocation clauses only)
- Affected projects: Cross-project

The platform should do the work, the user should act when there is something to act on. Loreloom uses a private Notion home page to coordinate context, decisions, tasks, and handoffs across authorized AI platforms. After acceptance and migration, Notion holds the canonical shared Loreloom policy. For shared workflow, the accepted policy takes precedence over conflicting repository instructions. Repository-specific build and testing rules remain in their repositories. Conflicts are reported explicitly rather than silently reconciled. This project policy does not override platform requirements or Glen's direct instructions. Short repository pointers identify where to retrieve the shared policy.

Each AI retrieves the current policy at task start and records the revision used. If retrieval fails, project actions pause. Glen controls acceptance and supersession of decision records. Authorized AIs may draft proposals and update task progress. Policy changes require Glen's approval.

Records use ADR structure with Context, Decision, and Consequences sections, combined with an RFC-style supersession workflow. Each record has a stable unique seven-digit number in one sequence across Loreloom projects, beginning at ADR-0000000. Numbers are allocated from a central register before drafting. Glen allocates numbers initially until simultaneous assignment can be prevented reliably. Record statuses are Unconfirmed, Proposed, Accepted, and Superseded. Each record identifies affected projects and carries Supersedes and Superseded by headers.

Use one shared task list across Loreloom projects, identifying the project for each task. Track Pending, Active, Blocked, Completed, Cancelled, and Unknown outcome work separately from decision-record status. Each active task has one responsible AI. Ownership changes require an explicit handoff. Use a reusable AI handoff template. Each handoff records the task, responsible AI, current status, next step, repository or worktree used, evidence links, verification outcome, and when the evidence was last checked.

Create new worktrees at /Users/gjb/work/<repo>-worktrees/<type>-<name>. This replaces the old repository-local worktrees/<type>/<name> convention. Do not require a repository-local worktrees directory or an exact three-word summary. Remove the worktree when its merge/push workflow is verified complete. Work lands on a branch. Completion is delivered by pushing the working branch and opening a pull request against main. The account that opens a pull request must remain subscribed to its notifications; GitHub automatically subscribes the author. Never commit directly to main, push to main, or merge without review.

Start from loreloom/CONTEXT.md. Compare other context files against it before retiring duplicates, preserving unique requirements and explicitly recording unresolved conflicts with source references. Keep commit discipline, attribution trailers, build, testing, and ledger rules repository-specific. The shared worktree location convention is the explicit exception and takes precedence over old repository-local location requirements.

Interview Glen about the context contents for clarifications and miscellaneous issues, one question at a time. The Todo for this interview links directly to the proposed policy draft. Glen accepted this coordination policy. Subsequent changes to its meaning follow the numbered replacement and approval workflow.

When Glen explicitly accepts an ADR, persistence is part of the acceptance workflow and does not require a separate request. The responsible AI must update or create the ADR in Notion, set its status to Accepted, record Glen as approver and the acceptance date, and add or update the entry in the central ADR register.

Always respond in markdown for lists or reports.

## HELO
- Write a single-sentence acknowledgement that you have read this document.
- Do not provide an outline of its contents.
- If you are seeing this application introduce itself as `orcli`, read `AGENTS.md.testing` if it exists.
- Do not advertise if it was not readable.

## Testing

Tests may be run and gates enforced as described in this repository's workflow.

## Formatting

This file should be one sentence per line, short instructions.

## Interaction
- Humans are your friends.
