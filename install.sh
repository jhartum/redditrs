#!/bin/sh
set -eu

# redditrs release installer
#
# Downloads a checksum-verified redditrs release archive and installs the
# binary into the requested BINDIR (auto-selected by default). Never uses
# sudo and never modifies the shell profile.
#
# Security notes:
# - The archive and checksums.txt are fetched from the same GitHub release
#   over HTTPS. The mandatory SHA-256 comparison verifies download INTEGRITY
#   (the archive matches the hashes published with the release); it is not
#   independent authenticity. Trust in the binary ultimately relies on
#   GitHub's HTTPS transport and repository security.
# - Only the exact 'redditrs' member is extracted, into a private temp dir;
#   nothing from the archive is ever written outside it.
# - All validation happens before any installation step; because the README
#   installs via download-then-execute (never curl | sh), a failed download
#   never reaches the installation (install.sh runs only after curl succeeds).
#
# Usage: install.sh [-v VERSION] [-b DIR] [-h]
#   -v VERSION   latest, vN.N.N, or N.N.N (default: latest; env VERSION)
#   -b DIR       install directory (default: auto-selected; env BINDIR)
#   -h           show help
#
# Test-only: REDDITRS_DOWNLOAD_BASE_URL overrides the release download base
# (default https://github.com/jhartum/redditrs/releases/download), used by
# scripts/test-install.sh with file:// fixtures.

OWNER=jhartum
REPO=redditrs
BINARY=redditrs
GITHUB="https://github.com/${OWNER}/${REPO}"
DOWNLOAD_BASE=${REDDITRS_DOWNLOAD_BASE_URL:-"${GITHUB}/releases/download"}

tmpdir=
tmpbin=

log() { printf '%s\n' "$*" 1>&2; }

is_command() {
	command -v "$1" >/dev/null 2>&1
}

cleanup() {
	if [ -n "$tmpdir" ]; then
		rm -rf "$tmpdir"
	fi
	if [ -n "$tmpbin" ]; then
		rm -f "$tmpbin"
	fi
}

usage() {
	cat 1>&2 <<EOF
Usage: $0 [options]

Install the checksum-verified redditrs release binary.

Options:
  -v VERSION   Version to install: latest, vN.N.N, or N.N.N (default: latest)
  -b DIR       Installation directory (default: auto-selected writable DIR)
  -h           Show this help and exit

Environment:
  VERSION      Default version, overridden by -v
  BINDIR       Default install directory, overridden by -b
EOF
}

# parse_args runs first so -h and option errors work without curl or a
# platform check. Env VERSION/BINDIR are defaults; CLI flags override.
parse_args() {
	VERSION=${VERSION:-latest}
	BINDIR=${BINDIR:-}

	while getopts "hv:b:" opt; do
		case "$opt" in
		v) VERSION="$OPTARG" ;;
		b) BINDIR="$OPTARG" ;;
		h)
			usage
			exit 0
			;;
		*)
			usage
			exit 2
			;;
		esac
	done
	shift $((OPTIND - 1))
	if [ "$#" -gt 0 ]; then
		log "unexpected argument: $1"
		usage
		exit 2
	fi
}

# validate_version: accept only stable N.N.N (no v, no prerelease, no suffix,
# no path/query characters). Slashes and anything outside [0-9.] are rejected
# by the case before awk pins the exact shape.
validate_version() {
	case "$1" in
	*[!0-9.]*) return 1 ;;
	esac
	printf '%s\n' "$1" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { exit 0 } { exit 1 }'
}

# resolve_version sets TAG to the canonical vN.N.N. URLs are only ever
# constructed from the validated TAG.
resolve_version() {
	version=$1

	if [ "$version" = "latest" ]; then
		effective=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
			"${GITHUB}/releases/latest")
		tag=${effective##*/}
		case "$tag" in
		v*) tag_v=${tag#v} ;;
		*) tag_v=$tag ;;
		esac
		if ! validate_version "$tag_v"; then
			log "latest release resolved to invalid tag: $tag"
			exit 1
		fi
		TAG="v${tag_v}"
		return
	fi

	case "$version" in
	v*) tag_v=${version#v} ;;
	*) tag_v=$version ;;
	esac
	if ! validate_version "$tag_v"; then
		log "invalid version: $version (expected latest, vN.N.N, or N.N.N)"
		exit 1
	fi
	TAG="v${tag_v}"
}

# hash_compute prints the lowercase sha256 of a file via the first tool found.
# Output is normalized with awk tolower so it matches checksums.txt entries
# regardless of the tool's hex case (openssl prints uppercase).
hash_compute() {
	target=$1
	if is_command sha256sum; then
		sha256sum "$target" | awk '{print tolower($1)}'
	elif is_command shasum; then
		shasum -a 256 "$target" | awk '{print tolower($1)}'
	elif is_command openssl; then
		openssl dgst -sha256 "$target" | awk '{print tolower($NF)}'
	else
		log "no sha256 tool found (need sha256sum, shasum, or openssl)"
		exit 1
	fi
}

# verify_checksum: require EXACTLY ONE well-formed checksums.txt entry for the
# asset (exactly two fields, digest of exactly 64 hex chars). Any duplicate,
# malformed, or missing entry aborts before extraction or installation.
verify_checksum() {
	target=$1
	checksums=$2
	asset=$3

	count=0
	want=
	while read -r chash cname rest; do
		if [ -z "$chash" ] && [ -z "$cname" ]; then
			continue
		fi
		if [ -z "$cname" ] || [ -n "$rest" ]; then
			log "malformed checksums.txt line (expected '<64-hex> <filename>'): '$chash $cname $rest'"
			return 1
		fi
		case "$chash" in
		*[!0-9a-fA-F]*)
			log "malformed digest in checksums.txt: '$chash'"
			return 1
			;;
		esac
		if [ "${#chash}" -ne 64 ]; then
			log "malformed digest length in checksums.txt: '$chash'"
			return 1
		fi
		if [ "$cname" = "$asset" ]; then
			count=$((count + 1))
			want=$(printf '%s\n' "$chash" | awk '{print tolower($0)}')
		fi
	done <"$checksums"

	if [ "$count" -eq 0 ]; then
		log "cannot find checksum for '$asset' in checksums.txt"
		return 1
	fi
	if [ "$count" -gt 1 ]; then
		log "duplicate checksum entries for '$asset' in checksums.txt"
		return 1
	fi

	got=$(hash_compute "$target")
	if [ "$want" != "$got" ]; then
		log "checksum mismatch for '$asset': expected $want, got $got"
		return 1
	fi
	log "checksum verified for ${asset}"
}

# extract_binary extracts ONLY the single root 'redditrs' member into dest.
# Any archive without exactly one such member is rejected; no other member is
# ever written, so nothing from the archive can land outside the temp dir.
extract_binary() {
	archive=$1
	dest=$2

	member=
	count=0
	for m in $(tar -tzf "$archive"); do
		case "$m" in
		"${BINARY}" | "./${BINARY}")
			count=$((count + 1))
			member=$m
			;;
		esac
	done

	if [ "$count" -eq 0 ]; then
		log "archive missing root '${BINARY}' member"
		return 1
	fi
	if [ "$count" -gt 1 ]; then
		log "archive contains duplicate '${BINARY}' members"
		return 1
	fi

	tar -xzf "$archive" -C "$dest" "$member"
}

# dir_usable: exists, writable, and searchable (executable bit).
dir_usable() {
	[ -d "$1" ] && [ -w "$1" ] && [ -x "$1" ]
}

# dir_secure rejects directories that another local user can modify. This
# makes the mktemp + rename installation safe from destination replacement by
# anyone other than the current user. ACLs are outside this portable check.
dir_secure() {
	secure_dir=$1
	secure_uid=$(id -u) || return 1
	case "$OS" in
	darwin)
		secure_owner=$(stat -f '%u' "$secure_dir") || return 1
		secure_mode=$(stat -f '%Lp' "$secure_dir") || return 1
		;;
	linux)
		secure_owner=$(stat -c '%u' "$secure_dir") || return 1
		secure_mode=$(stat -c '%a' "$secure_dir") || return 1
		;;
	*) return 1 ;;
	esac

	[ "$secure_owner" = "$secure_uid" ] || return 1
	case "$secure_mode" in
	'' | *[!0-7]*) return 1 ;;
	esac
	secure_mode_value=$((0$secure_mode))
	[ $((secure_mode_value & 022)) -eq 0 ]
}

dir_safe() {
	dir_usable "$1" && dir_secure "$1"
}

# in_path compares components literally; glob characters in directory names
# are not interpreted as patterns.
in_path() {
	path_target=$1
	path_rest=${PATH:-}
	while :; do
		case "$path_rest" in
		*:*)
			path_entry=${path_rest%%:*}
			path_rest=${path_rest#*:}
			path_last=no
			;;
		*)
			path_entry=$path_rest
			path_last=yes
			;;
		esac
		if [ "$path_entry" = "$path_target" ]; then
			return 0
		fi
		[ "$path_last" = yes ] && break
	done
	return 1
}

# select_bindir sets BINDIR_USED to an absolute, user-owned, writable,
# searchable directory that is not group/world-writable. Never uses sudo.
select_bindir() {
	if [ -n "$BINDIR" ]; then
		mkdir -p "$BINDIR"
		if ! BINDIR_USED=$(cd "$BINDIR" && pwd -P); then
			log "cannot resolve installation directory: $BINDIR"
			return 1
		fi
		if ! dir_usable "$BINDIR_USED"; then
			log "installation directory not writable/searchable: $BINDIR_USED"
			return 1
		fi
		if ! dir_secure "$BINDIR_USED"; then
			log "installation directory must be owned by the current user and not group/world-writable: $BINDIR_USED"
			return 1
		fi
		NEED_HINT=no
		return
	fi

	# Preferred candidates suppress the hint only if already in PATH.
	for d in "$HOME/.local/bin" "$HOME/bin" /opt/homebrew/bin /usr/local/bin; do
		if dir_safe "$d" && in_path "$d"; then
			BINDIR_USED=$d
			NEED_HINT=no
			return
		fi
	done

	# First safe absolute directory already in PATH. Parse components without
	# unquoted expansion so literal glob characters remain literal.
	path_scan=${PATH:-}
	while :; do
		case "$path_scan" in
		*:*)
			d=${path_scan%%:*}
			path_scan=${path_scan#*:}
			path_scan_last=no
			;;
		*)
			d=$path_scan
			path_scan_last=yes
			;;
		esac
		case "$d" in
		/*)
			if dir_safe "$d"; then
				BINDIR_USED=$d
				NEED_HINT=no
				return
			fi
			;;
		esac
		[ "$path_scan_last" = yes ] && break
	done

	# Fallback: create ~/.local/bin; hint when it is not on PATH.
	BINDIR_USED="$HOME/.local/bin"
	mkdir -p "$BINDIR_USED"
	if ! dir_usable "$BINDIR_USED"; then
		log "cannot create usable directory: $BINDIR_USED"
		return 1
	fi
	if ! dir_secure "$BINDIR_USED"; then
		log "fallback directory must be owned by the current user and not group/world-writable: $BINDIR_USED"
		return 1
	fi
	if in_path "$BINDIR_USED"; then
		NEED_HINT=no
	else
		NEED_HINT=yes
	fi
}

install_binary() {
	src=$1
	final=$2
	tmpbin=$(mktemp "${BINDIR_USED}/.${BINARY}.XXXXXX")
	if is_command install; then
		install -m 0755 "$src" "$tmpbin"
	else
		cp "$src" "$tmpbin"
		chmod 0755 "$tmpbin"
	fi
	mv -f "$tmpbin" "$final"
}

execute() {
	VERSION_NO_V=${TAG#v}
	ASSET="redditrs_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
	ASSET_URL="${DOWNLOAD_BASE}/${TAG}/${ASSET}"
	CHECKSUM_URL="${DOWNLOAD_BASE}/${TAG}/checksums.txt"

	log "installing ${BINARY} ${TAG} (${OS}/${ARCH}) into ${BINDIR_USED}"
	log "downloading ${ASSET_URL}"

	tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/redditrs-install.XXXXXX")

	curl -fsSL --retry 3 -o "${tmpdir}/${ASSET}" "${ASSET_URL}"
	curl -fsSL --retry 3 -o "${tmpdir}/checksums.txt" "${CHECKSUM_URL}"

	verify_checksum "${tmpdir}/${ASSET}" "${tmpdir}/checksums.txt" "${ASSET}"

	extract_binary "${tmpdir}/${ASSET}" "${tmpdir}"

	if [ ! -f "${tmpdir}/${BINARY}" ] || [ -L "${tmpdir}/${BINARY}" ] || [ ! -x "${tmpdir}/${BINARY}" ]; then
		log "extracted '${BINARY}' is not a regular executable file"
		return 1
	fi

	# Run the extracted, checksum-verified binary BEFORE installation.
	if ! version_out=$("${tmpdir}/${BINARY}" --version 2>&1); then
		log "binary '${BINARY}' failed to run (--version exited nonzero)"
		return 1
	fi

	install_binary "${tmpdir}/${BINARY}" "${BINDIR_USED}/${BINARY}"

	printf '%s\n' "installed: ${BINDIR_USED}/${BINARY}"
	printf '%s\n' "${version_out}"
	if [ "${NEED_HINT}" = "yes" ]; then
		printf '%s\n' "Add redditrs to your PATH: export PATH=\"${BINDIR_USED}:\$PATH\""
	fi
}

main() {
	trap cleanup 0
	trap 'exit 1' HUP INT TERM

	parse_args "$@"

	for dep in curl tar mktemp awk id stat; do
		if ! is_command "$dep"; then
			log "required command not found: $dep"
			log "install it (e.g. 'apt install curl') and rerun this script"
			exit 1
		fi
	done

	OS=$(uname -s)
	case "$OS" in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	*)
		log "unsupported OS: $OS (supported: darwin, linux)"
		exit 1
		;;
	esac

	ARCH=$(uname -m)
	case "$ARCH" in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*)
		log "unsupported architecture: $ARCH (supported: amd64, arm64)"
		exit 1
		;;
	esac

	resolve_version "$VERSION"
	select_bindir || exit 1
	execute
}

main "$@"
