---
schema: v1
---

## B-01KYE69HSJE8HB7PGDFMHKE6NN schema refusals name only the rejected property, never the accepted shape — the CLI should render the tool schema once on validation failures
kind: bug
state: draft
created: 2026-07-26

Found by the tricky judge rerun for T-01KYE5: judge U2 burned FOURTEEN decide refusals cycling choice, option, value, op=rescope and rescope:true without ever hearing that the accepted form is op=answer plus choose — the SDK validation message names the rejected property (unexpected additional properties, or missing properties) and stops, so the agent learns not-that fourteen times and never learns sondern-das. The same class cost the t-batch six decide refusals and every basic judge two draft refusals; the rule-specific shape line (T-01KYE5) halved rule refusals and thereby PROVED the pattern generalizes. Fix direction, generic rather than per-tool: the CLI call path already knows the tool name and holds a live session — on a refusal whose text starts with the SDK validating-arguments marker, fetch the tool input schema via tools/list (mcpclient gains a Schema(name) accessor, cached per session) and append ONE dense line, shape: <tool> {prop:type, ...} with required properties marked, before exiting non-zero. Cost only on schema refusals; MCP-protocol clients already receive schemas at list time and are untouched. VERIFY: a wrong-shape decide call over the CLI prints the shape line naming op and choose; unit test with a stub session pins the rendering and the only-on-validation-refusal condition; a tricky judge batch n=3 after landing shows decide refusals per run at or below three and batch validity restored to 3/3.
