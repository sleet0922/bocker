set -euo pipefail

source "${BASH_SOURCE[0]%/*}/bocker"

complete_words() {
    COMP_WORDS=(bocker "$@")
    COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
    _bocker_completion
}

contains() {
    local expected="$1" value
    for value in "${COMPREPLY[@]}"; do
        [[ "$value" == "$expected" ]] && return 0
    done
    return 1
}

complete_words ""
contains template
contains image
contains container

complete_words image ""
contains build
contains list
contains run
contains remove

complete_words container ""
contains shell
contains exec
contains set

complete_words image run --network ""
contains nat
contains bridge

complete_words template install --permission ""
contains normal
contains super

echo "Bash completion tests passed"
