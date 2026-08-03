#!/bin/sh
set -eu

# Hermetic end-to-end test for install.sh. No network access: builds a local
# fake v9.8.7 release (tar.gz + checksums.txt), serves it through
# REDDITRS_DOWNLOAD_BASE_URL=file://..., and exercises the installer's
# bindir/version/checksum handling.

VERSION_FAKE=9.8.7
TAG_FAKE="v${VERSION_FAKE}"
EXPECTED_VERSION_OUT="redditrs version ${VERSION_FAKE}"

repo_root=$(cd "$(dirname "$0")/.." && pwd -P)
installer="$repo_root/install.sh"

work=
failures=0

cleanup() {
	if [ -n "${work:-}" ]; then
		rm -rf "$work"
	fi
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

fail() {
	echo "FAIL: $*" 1>&2
	failures=$((failures + 1))
}

work=$(mktemp -d "${TMPDIR:-/tmp}/redditrs-test-install.XXXXXX")
releases="$work/releases/download"
mkdir -p "$releases/$TAG_FAKE"
download_base="file://$work/releases/download"

host_os=$(uname -s)
case "$host_os" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) echo "FAIL: unsupported test OS $host_os" 1>&2; exit 1 ;;
esac
host_arch=$(uname -m)
case "$host_arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) echo "FAIL: unsupported test arch $host_arch" 1>&2; exit 1 ;;
esac

ASSET="redditrs_${VERSION_FAKE}_${os}_${arch}.tar.gz"

# --- build the fake release ------------------------------------------------
srcdir="$work/src"
mkdir -p "$srcdir"
cat >"$srcdir/redditrs" <<EOF
#!/bin/sh
if [ "\${1:-}" = "--version" ]; then
	printf '%s\n' "$EXPECTED_VERSION_OUT"
	exit 0
fi
exit 0
EOF
chmod 0755 "$srcdir/redditrs"
(cd "$srcdir" && tar -czf "$releases/$TAG_FAKE/$ASSET" redditrs)

file_sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	fi
}

# Each argument becomes one line, so a duplicate test passes two distinct
# lines (never a malformed single 4-field line).
write_checksums() {
	printf '%s\n' "$@" >"$releases/$TAG_FAKE/checksums.txt"
}

hash=$(file_sha256 "$releases/$TAG_FAKE/$ASSET")
write_checksums "$hash  $ASSET"

# --- helpers ----------------------------------------------------------------
assert_ok() {
	desc=$1
	shift
	if env "$@" sh "$installer" >"$work/out.log" 2>&1; then
		echo "ok: $desc"
	else
		fail "$desc"
		cat "$work/out.log" 1>&2
	fi
}

assert_fail() {
	desc=$1
	shift
	if env "$@" sh "$installer" >"$work/out.log" 2>&1; then
		fail "$desc (installer unexpectedly succeeded)"
		cat "$work/out.log" 1>&2
	else
		echo "ok: $desc"
	fi
}

# --- 1. -h exits 0 without curl/deps ----------------------------------------
if sh "$installer" -h >"$work/h.log" 2>&1; then
	echo "ok: -h exits 0"
else
	fail "-h exits 0"
	cat "$work/h.log" 1>&2
fi

# --- 2. absolute BINDIR success ---------------------------------------------
abs_bin="$work/abs-bin"
assert_ok "absolute BINDIR install" \
	VERSION=$VERSION_FAKE BINDIR="$abs_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ ! -x "$abs_bin/redditrs" ]; then
	fail "absolute BINDIR: binary missing in $abs_bin"
	cat "$work/out.log" 1>&2
else
	got=$("$abs_bin/redditrs" --version)
	if [ "$got" = "$EXPECTED_VERSION_OUT" ]; then
		echo "ok: absolute BINDIR binary reports $got"
	else
		fail "absolute BINDIR: unexpected version '$got'"
	fi
fi

# --- 3. relative -b ./bin resolves at caller path ----------------------------
rel_dir="$work/rel"
mkdir -p "$rel_dir"
(
	cd "$rel_dir" &&
		env VERSION=$VERSION_FAKE REDDITRS_DOWNLOAD_BASE_URL="$download_base" \
			sh "$installer" -b ./bin >out.log 2>&1
)
if [ $? -eq 0 ] && [ -x "$rel_dir/bin/redditrs" ]; then
	echo "ok: relative -b ./bin installs at caller path"
else
	fail "relative -b ./bin"
	cat "$rel_dir/out.log" 2>&1
fi

# --- 4. existing ~/.local/bin outside PATH is not silently chosen ------------
home="$work/home"
mkdir -p "$home/.local/bin"
pathtmp="$work/pathtmp"
mkdir -p "$pathtmp"
assert_ok "existing ~/.local/bin outside PATH is not silently chosen" \
	HOME="$home" PATH="$pathtmp:/usr/bin:/bin" \
	VERSION=$VERSION_FAKE REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$home/.local/bin/redditrs" ]; then
	fail "installed into non-PATH ~/.local/bin"
	cat "$work/out.log" 1>&2
fi
if [ -x "$pathtmp/redditrs" ]; then
	echo "ok: binary landed in writable PATH dir $pathtmp"
else
	fail "expected binary in PATH dir $pathtmp"
	cat "$work/out.log" 1>&2
fi

# --- 5. unsafe shared destination is rejected --------------------------------
unsafe_bin="$work/unsafe-bin"
mkdir -p "$unsafe_bin"
chmod 0777 "$unsafe_bin"
assert_fail "world-writable BINDIR rejected" \
	VERSION=$VERSION_FAKE BINDIR="$unsafe_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if grep -q "not group/world-writable" "$work/out.log"; then
	echo "ok: unsafe BINDIR rejection explains required permissions"
else
	fail "unsafe BINDIR rejection did not explain required permissions"
	cat "$work/out.log" 1>&2
fi

# --- 6. PATH components containing glob characters compare literally ---------
home_glob="$work/home*"
mkdir -p "$home_glob/.local/bin"
path_glob_match="$work/homeXYZ/.local/bin"
mkdir -p "$path_glob_match"
assert_ok "glob characters in HOME path are treated literally" \
	HOME="$home_glob" PATH="$path_glob_match:/usr/bin:/bin" \
	VERSION=$VERSION_FAKE REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$home_glob/.local/bin/redditrs" ]; then
	fail "glob-containing HOME directory falsely matched PATH"
fi
if [ -x "$path_glob_match/redditrs" ]; then
	echo "ok: literal PATH component selected"
else
	fail "expected binary in literal PATH component $path_glob_match"
	cat "$work/out.log" 1>&2
fi

# --- 7. invalid versions rejected before any download/install ----------------
assert_fail "version with slash rejected" \
	VERSION='v1.2.3/../../payload' REDDITRS_DOWNLOAD_BASE_URL="$download_base"
assert_fail "prerelease suffix rejected" \
	VERSION='1.2.3-beta.1' REDDITRS_DOWNLOAD_BASE_URL="$download_base"
assert_fail "non-version rejected" \
	VERSION='abc' REDDITRS_DOWNLOAD_BASE_URL="$download_base"

# --- 8. checksum rejection cases ---------------------------------------------
DIGEST_64="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

cs_bin="$work/cs-missing"
write_checksums "$DIGEST_64  not_the_asset.tar.gz"
assert_fail "missing checksum entry rejected" \
	VERSION=$VERSION_FAKE BINDIR="$cs_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$cs_bin/redditrs" ]; then
	fail "missing checksum: binary must not be installed"
fi

cs_bin="$work/cs-malformed"
write_checksums "$DIGEST_64  extra_field  $ASSET"
assert_fail "malformed checksum line (3 fields) rejected" \
	VERSION=$VERSION_FAKE BINDIR="$cs_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$cs_bin/redditrs" ]; then
	fail "malformed checksum: binary must not be installed"
fi

cs_bin="$work/cs-badhex"
write_checksums "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz  $ASSET"
assert_fail "malformed digest (non-hex) rejected" \
	VERSION=$VERSION_FAKE BINDIR="$cs_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$cs_bin/redditrs" ]; then
	fail "malformed digest: binary must not be installed"
fi

cs_bin="$work/cs-dup"
write_checksums "$DIGEST_64  $ASSET" "$DIGEST_64  $ASSET"
assert_fail "duplicate checksum entries rejected" \
	VERSION=$VERSION_FAKE BINDIR="$cs_bin" REDDITRS_DOWNLOAD_BASE_URL="$download_base"
if [ -x "$cs_bin/redditrs" ]; then
	fail "duplicate checksum: binary must not be installed"
fi
if grep -q "duplicate checksum entries" "$work/out.log"; then
	echo "ok: duplicate rejection reports duplicate checksum entries"
else
	fail "duplicate rejection did not report 'duplicate checksum entries'"
	cat "$work/out.log" 1>&2
fi

# restore the good checksums for completeness
write_checksums "$hash  $ASSET"

# --- summary -----------------------------------------------------------------
if [ "$failures" -eq 0 ]; then
	echo "installer test: ok"
else
	echo "installer test: $failures failure(s)" 1>&2
	exit 1
fi
