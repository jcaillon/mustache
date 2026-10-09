#!/usr/bin/env bash
set -euo pipefail

function runMustacheTest() {
  local testFolder="${1}"
  shift 1
  go run ../main.go "${@}" "${testFolder}/data.yaml" "${testFolder}/template.mustache" >"${testFolder}/output" 2>"${testFolder}/error.log" || true
}

pushd "${BASH_SOURCE%/*}"

runMustacheTest "set-delimiters" --set-delimiters "<% %>" --
runMustacheTest "missing-variable" --strict
runMustacheTest "missing-section" --strict
