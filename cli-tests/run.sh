#!/usr/bin/env bash
set -euo pipefail

function runMustacheTest() {
  local testFolder="${1}"
  shift 1
  echo go run ../main.go "${@}" "${testFolder}/data.yaml" "${testFolder}/template.mustache"
  go run ../main.go "${@}" "${testFolder}/data.yaml" "${testFolder}/template.mustache" >"${testFolder}/output" 2>"${testFolder}/error.log" || true
}

function runTests() {
  local IFS testFolder
  for testFolder in ./*; do
    if [[ ! -d ${testFolder} ]]; then
      continue
    fi
    if [[ -f ${testFolder}/options ]]; then
      IFS='' read -rd '' REPLY <"${testFolder}/options" || :
    else
      REPLY=''
    fi
    REPLY="${REPLY%%[[:space:]]}"
    local -a args
    eval "args=(${REPLY})"
    # shellcheck disable=SC2086
    runMustacheTest "${testFolder#'./'}" "${args[@]}"
  done
}

pushd "${BASH_SOURCE%/*}"
runTests
