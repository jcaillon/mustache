#!/usr/bin/env bash
set -euo pipefail

function runMustacheTest() {
  local testFolder="${1}"
  shift 1
  go run ../main.go "${@}" "${testFolder}/data.yaml" "${testFolder}/template.mustache" &>"${testFolder}/output.txt" || true
}

pushd "${BASH_SOURCE%/*}"

runMustacheTest "1-set-delimiters" --set-delimiters "<% %>" --
runMustacheTest "2-missing-variable"
