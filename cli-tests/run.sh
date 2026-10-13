#!/usr/bin/env bash
set -euo pipefail

function runMustacheTest() {
  local testFolder="${1}"
  shift 1
  local -a args=(run main.go "${@}" "${testFolder}/data.yaml" "${testFolder}/template.mustache")
  printf '%q ' "go" "${args[@]}"
  printf '\n'
  go "${args[@]}" >"${testFolder}/output" 2>"${testFolder}/error.log" || true

  if [[ -s "${testFolder}/error.log" ]]; then
    git diff "${testFolder}/error.log"
  else
    rm -f "${testFolder}/error.log"
  fi
  if [[ -s "${testFolder}/output" ]]; then
    git diff "${testFolder}/output"
  else
    rm -f "${testFolder}/output"
  fi
}

function runTests() {
  local IFS testFolder
  for testFolder in "./${BASH_SOURCE%/*}"/*; do
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

runTests
