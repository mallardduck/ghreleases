#!/usr/bin/env bash
# Reads changed file paths on stdin and prints which CI jobs should run, as
# GitHub Actions outputs: core, pgp, sigstore and integration (true/false).
#
#   core         anything outside the nested modules (root package, docs, testdata, ...)
#   pgp          pgp/**
#   sigstore     sigstore/**
#   integration  runs whenever anything it exercises changes
#
# Anything that could change how CI itself behaves, and any diff we cannot read,
# runs everything: skipping tests is only safe when we know what changed.
set -euo pipefail

files=$(cat)

has() { printf '%s\n' "$files" | grep -Eq "$1"; }

all=false
if [ -z "$files" ] || has '^\.github/|^go\.work'; then
  all=true
fi

core=$all pgp=$all sigstore=$all
if printf '%s\n' "$files" | grep -Ev '^(pgp|sigstore|integration)/' | grep -q .; then core=true; fi
if has '^pgp/'; then pgp=true; fi
if has '^sigstore/'; then sigstore=true; fi

integration=$all
if [ "$core" = true ] || [ "$pgp" = true ] || [ "$sigstore" = true ] || has '^integration/'; then
  integration=true
fi

echo "core=$core"
echo "pgp=$pgp"
echo "sigstore=$sigstore"
echo "integration=$integration"
