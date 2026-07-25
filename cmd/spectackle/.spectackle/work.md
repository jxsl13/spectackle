---
schema: v1
---

## B-01KYDRZHRMER0AQ62QPAFRJRK0 writePIDFile is non-atomic: the pidfile is observable empty between create and write
kind: bug
state: done
created: 2026-07-25

Root cause, confirmed by reading writePIDFile in cmd/spectackle/main.go: the function opens the path with O_CREATE|O_EXCL, then writes the PID, then closes. Between OpenFile and Write the file exists with zero bytes. Any watcher that acts on file appearance reads an empty pidfile. Observed: TestServePidfileHTTPCreateAndRemove failed in a full go test -race run at 0.10s, far below its 2s waitForFile budget — the fast-fail signature of waitForFile returning on the first successful Stat and the content assertion reading empty bytes. Under parallel suite load the create-to-write window stretches, which is why the race only fires on a loaded machine. This is a production defect, not a test defect: shell watchers that cat the pidfile the moment it appears hit the same window. Fix: write the PID to a temp file in the same directory, then os.Link(tmp, path) — appearance becomes atomic with full content, and EEXIST from link preserves the refuse-to-clobber contract that TestServePidfilePreExisting pins (pre-existing file left untouched, non-zero exit). Always remove the temp file. VERIFY: go test -race -count=20 ./cmd/spectackle passes; the three pidfile tests unchanged.
