---
schema: v1
---

## B-01KYDKNQRWF719MBA8Y234KF88 automation commits carry a hardcoded spectackle@localhost identity, so they are unverifiable and misattributed
kind: bug
state: active
created: 2026-07-25

Field report: commits are unverified and the committer email looks wrong. Verified in the log: every commit the automation makes — code, checkpoint, records — carries author and committer spectackle <spectackle@localhost>, because the git exec helper (internal/wt/wt.go line 23) injects -c user.name and -c user.email into EVERY command it runs, and the offline forge merge duplicates the same override.

WHY IT IS WRONG twice over. Attribution: the work was driven by a person whose repository this is, and their git config carries the identity they chose; a hardcoded placeholder erases that. Verification: GitHub marks a commit Verified when it is signed by a key attached to the account owning the email — spectackle@localhost can never be that, so the automation structurally produces Unverified commits even on a machine fully configured for signing, and the identity override also bypasses whatever commit.gpgsign the user configured at the exec boundary.

FIX DIRECTION: the helper inherits the repository and global git config — identity AND signing — by simply not overriding it. The hardcoded identity exists so tests never depend on host config; that concern moves into the test fixtures, which set user.name and user.email on their scratch repositories at init. For a host with no identity configured at all, degrade rather than fail: on the specific tell-me-who-you-are failure, retry once with the placeholder identity and say so in the result — never silently, per ADR-01KYDG.

VERIFY: on a repository whose git config carries a real identity, an automation commit carries that identity and, where signing is configured, is signed; the test fixtures pass on a host with no global git config; the no-identity fallback fires only on the specific failure and reports itself.
