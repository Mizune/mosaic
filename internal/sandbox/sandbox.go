// Package sandbox turns an agent launch command into one that runs
// inside an optional isolation layer (currently Docker). It is a
// thin command transform: given (spec, session id, workdir, env,
// argv) it returns the argv that tmux should actually spawn.
//
// Design notes:
//
//   - Mode=none always returns the original argv unchanged so the
//     non-sandboxed fast path is a no-op.
//   - The wrapper is a pure function — it does not talk to the
//     Docker daemon. Callers invoke docker (e.g. `docker rm`)
//     themselves at the appropriate lifecycle moments.
//   - The container name is deterministic (mosaic-<sessionID>) so
//     Stop/Delete can clean up without a roundtrip to sqlite.
package sandbox
