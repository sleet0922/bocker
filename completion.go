package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var completionShells = []string{"bash", "zsh", "fish"}

// CmdCompletion prints or installs shell completion. Installation is system
// wide because Bocker itself runs as root.
func CmdCompletion(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(completionUsage())
		return nil
	}
	if args[0] == "install" {
		shell := ""
		if len(args) > 2 {
			return fmt.Errorf("completion install accepts at most one shell")
		}
		if len(args) == 2 {
			shell = args[1]
		} else {
			shell = detectCompletionShell()
		}
		script, err := completionScript(shell)
		if err != nil {
			return err
		}
		path, err := completionInstallPath(shell)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create completion directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			return fmt.Errorf("install completion: %w", err)
		}
		fmt.Printf("Installed %s completion: %s\nOpen a new shell to enable it.\n", shell, path)
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("completion accepts one shell: bash, zsh, or fish")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

// ensureCompletionInstalled installs the current shell's completion file on
// first use. Existing files are left untouched; `completion install` is the
// explicit overwrite path.
func ensureCompletionInstalled() {
	if runtime.GOOS != "linux" {
		return
	}
	shell := detectCompletionShell()
	path, err := completionInstallPath(shell)
	if err != nil {
		return
	}
	if _, err := os.Stat(path); err != nil {
		script, scriptErr := completionScript(shell)
		if scriptErr == nil {
			if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr == nil {
				_ = os.WriteFile(path, []byte(script), 0o644)
			}
		}
	}
	ensureCompletionStartup(shell, path)
}

func ensureCompletionStartup(shell, path string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	var rc, block string
	switch shell {
	case "bash":
		rc = filepath.Join(home, ".bashrc")
		block = fmt.Sprintf("\n# bocker: automatic shell completion\nif [ -r '%s' ]; then . '%s'; fi\n", path, path)
	case "zsh":
		rc = filepath.Join(home, ".zshrc")
		block = fmt.Sprintf("\n# bocker: automatic shell completion\nfpath=(%s $fpath)\nautoload -Uz compinit && compinit\n", filepath.Dir(path))
	default:
		return
	}
	data, err := os.ReadFile(rc)
	if err == nil && strings.Contains(string(data), "# bocker: automatic shell completion") {
		return
	}
	f, err := os.OpenFile(rc, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(block)
}

func completionUsage() string {
	return `bocker completion - shell tab completion

Completion is installed and registered automatically on the first bocker run.
Open a new shell once after the first run.

Usage:
  bocker completion bash|zsh|fish
  bocker completion install [bash|zsh|fish]

Examples:
  source <(bocker completion bash)
  bocker completion install bash
`
}

func detectCompletionShell() string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	shell = strings.TrimPrefix(filepath.Base(shell), "-")
	for _, candidate := range completionShells {
		if shell == candidate {
			return shell
		}
	}
	return "bash"
}

func completionInstallPath(shell string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return "/etc/bash_completion.d/bocker", nil
	case "zsh":
		return "/usr/share/zsh/site-functions/_bocker", nil
	case "fish":
		return "/etc/fish/completions/bocker.fish", nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(completionShells, ", "))
	}
}

func completionScript(shell string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return bashCompletion, nil
	case "zsh":
		return zshCompletion, nil
	case "fish":
		return fishCompletion, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(completionShells, ", "))
	}
}

// CmdCompletionCandidates is intentionally undocumented: completion scripts
// use it to retrieve dynamic container and image names without parsing tables.
func CmdCompletionCandidates(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("completion candidate type is required")
	}
	client := NewIncusClient()
	var names []string
	switch args[0] {
	case "containers":
		containers, err := client.ListContainers()
		if err != nil {
			return err
		}
		for _, container := range containers {
			names = append(names, container.Name)
		}
	case "images":
		aliases, err := client.ListLocalImageAliases()
		if err != nil {
			return err
		}
		names = append(names, aliases...)
	default:
		return fmt.Errorf("unknown completion candidate type %q", args[0])
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

const bashCompletion = `# bash completion for bocker
_bocker_completion_containers=""
_bocker_completion_images=""
_bocker_completion_containers_at=-3
_bocker_completion_images_at=-3
_bocker_completion_refresh_containers() {
    if (( SECONDS - _bocker_completion_containers_at >= 2 )); then
        _bocker_completion_containers="$(bocker __complete containers 2>/dev/null)"
        _bocker_completion_containers_at=$SECONDS
    fi
}
_bocker_completion_refresh_images() {
    if (( SECONDS - _bocker_completion_images_at >= 2 )); then
        _bocker_completion_images="$(bocker __complete images 2>/dev/null)"
        _bocker_completion_images_at=$SECONDS
    fi
}
_bocker_completion() {
    local cur cmd cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    cmd="${COMP_WORDS[1]}"
    cword="$COMP_CWORD"
    local commands="list ls start stop restart in exec set export import install i remove rm uninstall build create run images image completion help version"

    if (( cword == 1 )); then
        COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
        return
    fi
    case "$cmd" in
        start|stop|restart|in|exec|export|uninstall)
            if (( cword == 2 )); then _bocker_completion_refresh_containers; COMPREPLY=( $(compgen -W "$_bocker_completion_containers" -- "$cur") ); fi ;;
        set)
            if (( cword == 2 )); then
                _bocker_completion_refresh_containers
                COMPREPLY=( $(compgen -W "$_bocker_completion_containers" -- "$cur") )
            elif (( cword == 3 )); then
                COMPREPLY=( $(compgen -W "port domain host dns autostart network net" -- "$cur") )
            elif [[ "${COMP_WORDS[3]}" == "network" || "${COMP_WORDS[3]}" == "net" ]]; then
                COMPREPLY=( $(compgen -W "bridge nat" -- "$cur") )
            elif [[ "${COMP_WORDS[3]}" == "autostart" ]]; then
                COMPREPLY=( $(compgen -W "on off true false" -- "$cur") )
            elif [[ "${COMP_WORDS[3]}" == "port" && $cword -eq 4 ]]; then
                COMPREPLY=( $(compgen -W "list rm" -- "$cur") )
            fi ;;
        remove|rm)
            if (( cword == 2 )); then
                COMPREPLY=( $(compgen -W "container image" -- "$cur") )
            elif [[ "${COMP_WORDS[2]}" == "container" ]]; then
                _bocker_completion_refresh_containers
                COMPREPLY=( $(compgen -W "$_bocker_completion_containers" -- "$cur") )
            elif [[ "${COMP_WORDS[2]}" == "image" ]]; then
                _bocker_completion_refresh_images
                COMPREPLY=( $(compgen -W "$_bocker_completion_images" -- "$cur") )
            fi ;;
        build)
            if (( cword == 2 )); then COMPREPLY=( $(compgen -W "--name --network --help show" -- "$cur") ); fi ;;
        create|run)
            if (( cword == 2 )); then COMPREPLY=( $(compgen -W "--network --permission" -- "$cur") ); fi
            if [[ "${COMP_WORDS[cword-1]}" == "--network" ]]; then COMPREPLY=( $(compgen -W "bridge nat" -- "$cur") ); fi
            if [[ "${COMP_WORDS[cword-1]}" == "--permission" ]]; then COMPREPLY=( $(compgen -W "normal super" -- "$cur") ); fi ;;
        install|i|import)
            if (( cword == 2 )); then COMPREPLY=( $(compgen -W "--network --permission" -- "$cur") ); fi
            if [[ "${COMP_WORDS[cword-1]}" == "--network" ]]; then COMPREPLY=( $(compgen -W "bridge nat" -- "$cur") ); fi
            if [[ "${COMP_WORDS[cword-1]}" == "--permission" ]]; then COMPREPLY=( $(compgen -W "normal super" -- "$cur") ); fi ;;
        completion)
            if (( cword == 2 )); then COMPREPLY=( $(compgen -W "bash zsh fish install" -- "$cur") ); fi
            if [[ "${COMP_WORDS[2]}" == "install" && $cword -eq 3 ]]; then COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ); fi ;;
    esac
}
complete -F _bocker_completion bocker
`

const zshCompletion = `#compdef bocker
typeset -ga _bocker_cached_containers=()
typeset -ga _bocker_cached_images=()
typeset -gi _bocker_containers_at=-3
typeset -gi _bocker_images_at=-3
_bocker_refresh_containers() {
  if (( SECONDS - _bocker_containers_at >= 2 )); then
    _bocker_cached_containers=("${(@f)$(bocker __complete containers 2>/dev/null)}")
    _bocker_containers_at=$SECONDS
  fi
}
_bocker_refresh_images() {
  if (( SECONDS - _bocker_images_at >= 2 )); then
    _bocker_cached_images=("${(@f)$(bocker __complete images 2>/dev/null)}")
    _bocker_images_at=$SECONDS
  fi
}
_bocker() {
  local -a commands
  commands=(
    'list:list containers' 'ls:list containers' 'start:start a container' 'stop:stop a container'
    'restart:restart a container' 'in:open a shell' 'exec:run a command' 'set:change settings'
    'export:export a container' 'import:import a backup' 'install:install an image'
    'remove:remove a container or image' 'rm:remove a container or image' 'uninstall:remove a container'
    'build:build an image'
    'create:create from Incusfile' 'run:alias for create' 'images:list local images'
    'completion:install or print completion' 'help:show help' 'version:show version'
  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'bocker command' commands
    return
  fi
  case $words[2] in
    (start|stop|restart|in|exec|export|uninstall) _bocker_refresh_containers; _describe -t containers container _bocker_cached_containers ;;
    (set)
      if (( CURRENT == 3 )); then
        _bocker_refresh_containers
        _describe -t containers container _bocker_cached_containers
      elif [[ $words[4] == network || $words[4] == net ]]; then
        _values 'network' bridge nat
      elif [[ $words[4] == autostart ]]; then
        _values 'autostart' on off true false
      elif [[ $words[4] == port && CURRENT == 5 ]]; then
        _values 'port action' list rm
      else
        _values 'setting' port domain host dns autostart network net
      fi ;;
    (remove|rm) if (( CURRENT == 3 && $words[3] == container )); then _bocker_refresh_containers; _describe -t containers container _bocker_cached_containers; elif (( CURRENT == 3 && $words[3] == image )); then _bocker_refresh_images; _describe -t images image _bocker_cached_images; else _values 'target type' container image; fi ;;
    (completion) _values 'shell' bash zsh fish install ;;
    (*) _files ;;
  esac
}
_bocker "$@"
`

const fishCompletion = `function __bocker_containers
    set -l now (date +%s)
    if not set -q __bocker_containers_at; or test (math $now - $__bocker_containers_at) -ge 2
        set -g __bocker_containers_cache (bocker __complete containers 2>/dev/null)
        set -g __bocker_containers_at $now
    end
    printf '%s\\n' $__bocker_containers_cache
end
function __bocker_images
    set -l now (date +%s)
    if not set -q __bocker_images_at; or test (math $now - $__bocker_images_at) -ge 2
        set -g __bocker_images_cache (bocker __complete images 2>/dev/null)
        set -g __bocker_images_at $now
    end
    printf '%s\\n' $__bocker_images_cache
end
complete -c bocker -f
complete -c bocker -n '__fish_use_subcommand' -a 'list ls start stop restart in exec set export import install i remove rm uninstall build create run images image completion help version'
complete -c bocker -n '__fish_seen_subcommand_from start stop restart in exec export uninstall' -a '(__bocker_containers)'
complete -c bocker -n '__fish_seen_subcommand_from set' -a '(__bocker_containers) port domain host dns autostart network net'
complete -c bocker -n '__fish_seen_subcommand_from set; and __fish_prev_arg_in network net' -a 'bridge nat'
complete -c bocker -n '__fish_seen_subcommand_from set; and __fish_prev_arg_in autostart' -a 'on off true false'
complete -c bocker -n '__fish_seen_subcommand_from set; and __fish_prev_arg_in port' -a 'list rm'
complete -c bocker -n '__fish_seen_subcommand_from remove rm' -a 'container image (__bocker_containers) (__bocker_images)'
complete -c bocker -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish install'
complete -c bocker -n '__fish_seen_subcommand_from build' -l name -r
complete -c bocker -n '__fish_seen_subcommand_from build' -l network -a 'bridge nat'
complete -c bocker -n '__fish_seen_subcommand_from create run install import' -l network -a 'bridge nat'
`
