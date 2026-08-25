#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
task_tmp=${TMPDIR:-"${repo_root}/.tmp"}
run_dir="${task_tmp%/}/hexxladb-pilot-soak"
report_dir="${repo_root}/.tmp/evidence"
report_path="${report_dir}/pilot-soak.json"

case "${run_dir}" in
  /|/hexxladb-pilot-soak)
    echo "pilot soak: refusing unsafe work directory ${run_dir}" >&2
    exit 2
    ;;
esac

if [[ -e "${run_dir}" || -L "${run_dir}" ]]; then
  echo "pilot soak: ${run_dir} already exists; inspect and remove this stale run explicitly" >&2
  exit 2
fi

mkdir -p "${run_dir}" "${report_dir}"
cleanup() {
  if [[ -d "${run_dir}" && ! -L "${run_dir}" ]]; then
    rm -rf --one-file-system -- "${run_dir}"
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "pilot soak: work directory ${run_dir} (removed on exit; source recovery set capped at 2 GiB)" >&2
echo "pilot soak: report ${report_path}" >&2
cd "${repo_root}"
revision=$(git rev-parse HEAD)
modified=false
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  modified=true
fi
go run ./examples/pilot_soak -work-dir "${run_dir}" "$@" -vcs-revision "${revision}" -vcs-modified "${modified}" | tee "${report_path}"
