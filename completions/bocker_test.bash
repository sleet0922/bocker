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

complete_words template install --
if contains --permission; then
	echo "removed --permission flag is still offered" >&2
	exit 1
fi

complete_words image build --
contains --build-arg

complete_words image build --build-arg ""
[[ ${#COMPREPLY[@]} -eq 0 ]]

complete_words container set demo ""
contains domain
contains port
contains mount
contains autostart
contains network

complete_words container set demo mount ""
contains list
contains add
contains update
contains rm
contains remove

complete_words container set demo mount add /tmp /var/lib ""
contains ro
contains rw

complete_words container set demo mount update mount-target ""
contains ro
contains rw

echo "Bash completion tests passed"
