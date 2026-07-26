---
schema: v1
---

## T-01KYE0553VFWMB1NJD9SECFTEC tool refusals over the CLI drop the transport blurb: the refusal text is the signal, the spawn paths are noise
kind: task
state: active
created: 2026-07-26

Found by the first live judge runs (T-01KYDZ archive note): every schema or tool refusal reaching a CLI-driven agent costs about 520 bytes, of which about 430 are the mcpclient error wrapper — tool X reported an error via stdio spawn <absolute binary path> serve -root <absolute workspace path> — repeated verbatim on every refusal. The wrapper duplicates zero information: the rendered result text IS the refusal, and the transport description exists for CONNECTION failures where no result text exists. Judge A paid seven of these (78 percent of its whole tool-output diet), judge B five (68 percent). MCP-protocol consumers never see this (IsError plus text only), and the scripted bench drops stderr, which is why only the agent-judge stage could catch it — the exact division of labor P-01KYDP predicted. Change, two sites: internal/mcpclient Call keeps the full transport description on TRANSPORT errors (CallTool returning err) but shrinks the IsError branch to tool <name> refused — the API contract (text plus non-nil error) is unchanged, TestCallIsErrorPath still passes; cmd/spectackle call printing suppresses the redundant stderr error line entirely when a refusal already printed its text, keeping the non-zero exit code as the machine signal. VERIFY: unit tests pin the refusal error text carries no paths and the transport-error text still does; judge rerun — two fresh haiku agents over agent-prep workspaces against the candidate — shows refusal cost near 90 bytes and a total diet reduction majority-driven by this change; scripted bench A/B expected tie (stdout-only metering), stated in the note.
