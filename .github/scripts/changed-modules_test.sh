#!/usr/bin/env bash
# Table test for changed-modules.sh: which CI jobs run for which changed files.
set -u
cd "$(dirname "$0")"

fail=0
check() { # name, expected "core pgp sigstore integration", then changed files on stdin
  local name=$1 want=$2 got
  got=$(./changed-modules.sh | sort | tr '\n' ' ' | sed 's/ $//')
  want=$(for kv in $want; do echo "$kv"; done | sort | tr '\n' ' ' | sed 's/ $//')
  if [ "$got" != "$want" ]; then
    echo "FAIL: $name"; echo "  got:  $got"; echo "  want: $want"; fail=1
  else
    echo "ok:   $name"
  fi
}

check "core source" "core=true pgp=false sigstore=false integration=true" <<'EOF'
client.go
download_test.go
EOF
check "core README only" "core=true pgp=false sigstore=false integration=true" <<'EOF'
README.md
EOF
check "pgp only" "core=false pgp=true sigstore=false integration=true" <<'EOF'
pgp/verifier.go
pgp/testdata/helm-v4.3.1/KEYS
EOF
check "sigstore only" "core=false pgp=false sigstore=true integration=true" <<'EOF'
sigstore/verifier.go
EOF
check "integration only" "core=false pgp=false sigstore=false integration=true" <<'EOF'
integration/verify_test.go
EOF
check "pgp and sigstore" "core=false pgp=true sigstore=true integration=true" <<'EOF'
pgp/verifier.go
sigstore/verifier.go
EOF
check "core and pgp" "core=true pgp=true sigstore=false integration=true" <<'EOF'
client.go
pgp/verifier.go
EOF
check "workflow change runs everything" "core=true pgp=true sigstore=true integration=true" <<'EOF'
.github/workflows/ci.yml
EOF
check "script change runs everything" "core=true pgp=true sigstore=true integration=true" <<'EOF'
.github/scripts/changed-modules.sh
EOF
check "go.work runs everything" "core=true pgp=true sigstore=true integration=true" <<'EOF'
go.work
EOF
check "unknown (empty) diff fails safe" "core=true pgp=true sigstore=true integration=true" </dev/null
check "a lookalike directory is core" "core=true pgp=false sigstore=false integration=true" <<'EOF'
pgpx/file.go
EOF

exit $fail
