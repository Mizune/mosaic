#!/usr/bin/env bash
# Seed a dirty git repo for diff-view.tape so the Diff View
# has something real to render. Idempotent: nukes and recreates
# /tmp/mosaic-diff-repo on every invocation.

set -euo pipefail

REPO=/tmp/mosaic-diff-repo
rm -rf "$REPO"
git init -q --initial-branch=main "$REPO"
cd "$REPO"
git config user.email t@e
git config user.name t

cat > main.go <<'GO'
package main

func main() {
	println("hi")
}
GO

git add main.go
git commit -q -m init

cat > main.go <<'GO'
package main

import "fmt"

func main() {
	fmt.Println("hello from diff demo")
}
GO

echo "notes" > NOTES.md
