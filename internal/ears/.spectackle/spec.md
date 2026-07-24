---
schema: v0
prefix: SPX-EARS
---

# EARS linter contracts

## SPX-EARS-001
IF a rule sentence lacks the mandatory response keyword, THEN the linter SHALL report finding E001 at the rule heading line.

## SPX-EARS-002
The linter SHALL classify every rule into exactly one of the 6 EARS patterns or report finding E002.

## SPX-EARS-003
The linter SHALL match EARS keywords case-sensitively so that lowercase words inside code fragments such as `if (i < n)` never trigger a pattern.

## SPX-EARS-004 {applies: go:ears.LintSentence,go:ears.ParseRules,go:ears.StripFrontMatter}
The EARS linter SHALL process arbitrary byte input in LintSentence, ParseRules and StripFrontMatter without panicking.
