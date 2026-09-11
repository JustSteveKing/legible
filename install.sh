#!/usr/bin/env sh

# legible installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/JustSteveKing/legible/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/JustSteveKing/legible/main/install.sh | sh -s -- --version v0.1.0
#
# Downloads a release built with GoReleaser from
# https://github.com/JustSteveKing/legible/releases, checks it against the
# release's checksums.txt, and installs the `legible` binary into a directory
# on your PATH.

set -eu
printf '\n'

BOLD="$(tput bold 2>/dev/null || printf '')"
GREY="$(tput setaf 0 2>/dev/null || printf '')"
UNDERLINE="$(tput smul 2>/dev/null || printf '')"
RED="$(tput setaf 1 2>/dev/null || printf '')"
GREEN="$(tput setaf 2 2>/dev/null || printf '')"
YELLOW="$(tput setaf 3 2>/dev/null || printf '')"
BLUE="$(tput setaf 4 2>/dev/null || printf '')"
MAGENTA="$(tput setaf 5 2>/dev/null || printf '')"
NO_COLOR="$(tput sgr0 2>/dev/null || printf '')"

# GoReleaser project name (the `{{ .ProjectName }}` in the archive template).
PROJECT_NAME="legible"
# Binary name installed onto the PATH.
BIN_NAME="legible"
GITHUB_REPO="JustSteveKing/legible"

# Supported GoReleaser os/arch combinations: "<os>/<arch>".
# Mirrors goos/goarch in .goreleaser.yaml (amd64, arm64 only).
SUPPORTED_TARGETS="darwin/amd64 darwin/arm64 \
                   linux/amd64 linux/arm64 \
                   windows/amd64 windows/arm64"

info() {
	printf '%s\n' "${BOLD}${GREY}>${NO_COLOR} $*"
}

warn() {
	printf '%s\n' "${YELLOW}! $*${NO_COLOR}"
}

error() {
	printf '%s\n' "${RED}x $*${NO_COLOR}" >&2
}

completed() {
	printf '%s\n' "${GREEN}+${NO_COLOR} $*"
}

has() {
	command -v "$1" 1>/dev/null 2>&1
}

curl_is_snap() {
	curl_path="$(command -v curl)"
	case "$curl_path" in
	/snap/*) return 0 ;;
	*) return 1 ;;
	esac
}

# Make sure user is not using zsh or non-POSIX-mode bash, which can cause issues
verify_shell_is_posix_or_exit() {
	if [ -n "${ZSH_VERSION+x}" ]; then
		error "Running installation script with \`zsh\` is known to cause errors."
		error "Please use \`sh\` instead."
		exit 1
	elif [ -n "${BASH_VERSION+x}" ] && [ -z "${POSIXLY_CORRECT+x}" ]; then
		error "Running installation script with non-POSIX \`bash\` may cause errors."
		error "Please use \`sh\` instead."
		exit 1
	else
		true # No-op: no issues detected
	fi
}

get_tmpfile() {
	suffix="$1"
	if has mktemp; then
		printf "%s.%s" "$(mktemp)" "${suffix}"
	else
		# No really good options here--let's pick a default + hope
		printf "/tmp/%s.%s" "${PROJECT_NAME}" "${suffix}"
	fi
}

# Test if a location is writable by trying to write to it. Windows does not let
# you test writeability other than by writing: https://stackoverflow.com/q/1999988
test_writable() {
	path="${1:-}/test.txt"
	if touch "${path}" 2>/dev/null; then
		rm "${path}"
		return 0
	else
		return 1
	fi
}

download() {
	file="$1"
	url="$2"

	if has curl && curl_is_snap; then
		warn "curl installed through snap cannot download ${PROJECT_NAME}."
		warn "Searching for other HTTP download programs..."
	fi

	if has curl && ! curl_is_snap; then
		cmd="curl --fail --silent --location --output $file $url"
	elif has wget; then
		cmd="wget --quiet --output-document=$file $url"
	elif has fetch; then
		cmd="fetch --quiet --output=$file $url"
	else
		error "No HTTP download program (curl, wget, fetch) found, exiting..."
		return 1
	fi

	$cmd && return 0 || rc=$?

	error "Command failed (exit code $rc): ${BLUE}${cmd}${NO_COLOR}"
	printf "\n" >&2
	info "This is likely due to ${PROJECT_NAME} not yet supporting your platform,"
	info "or the requested version/asset not existing."
	info "If you would like to see a build for your configuration,"
	info "please create an issue requesting a build for ${MAGENTA}${TARGET}${NO_COLOR}:"
	info "${BOLD}${UNDERLINE}https://github.com/${GITHUB_REPO}/issues/new${NO_COLOR}"
	return "$rc"
}

# Print the contents of a URL to stdout (used for the GitHub releases API).
fetch_stdout() {
	url="$1"
	if has curl && ! curl_is_snap; then
		curl --fail --silent --location "$url"
	elif has wget; then
		wget --quiet --output-document=- "$url"
	elif has fetch; then
		fetch --quiet --output=- "$url"
	else
		error "No HTTP download program (curl, wget, fetch) found, exiting..."
		return 1
	fi
}

# Resolve the "latest" release tag via the GitHub API.
resolve_latest_tag() {
	api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
	body="$(fetch_stdout "$api_url")" || return 1
	# Extract the first "tag_name": "..." value without requiring jq.
	tag="$(printf '%s' "$body" |
		grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' |
		head -n 1 |
		sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
	if [ -z "$tag" ]; then
		return 1
	fi
	printf '%s' "$tag"
}

# Compute the sha256 of a file using whatever tool is available.
compute_sha256() {
	file="$1"
	if has sha256sum; then
		sha256sum "$file" | awk '{ print $1 }'
	elif has shasum; then
		shasum -a 256 "$file" | awk '{ print $1 }'
	elif has openssl; then
		openssl dgst -sha256 "$file" | awk '{ print $NF }'
	else
		return 1
	fi
}

# Verify the downloaded archive against checksums.txt from the release.
# $1 = path to the downloaded archive on disk
# $2 = the archive's release filename as listed in checksums.txt
verify_checksum() {
	archive_path="$1"
	archive_name="$2"

	if [ -n "${NO_VERIFY-}" ]; then
		warn "Skipping checksum verification (--no-verify)."
		return 0
	fi

	actual="$(compute_sha256 "$archive_path")" || {
		warn "No sha256 tool (sha256sum, shasum, openssl) found."
		warn "Skipping checksum verification."
		return 0
	}

	checksums_url="${BASE_URL}/download/${TAG}/checksums.txt"
	checksums="$(fetch_stdout "$checksums_url")" || {
		error "Could not download checksums.txt from the release."
		error "Re-run with --no-verify to bypass (not recommended)."
		return 1
	}

	# Lines look like: "<sha256>  legible_0.1.0_darwin_arm64.tar.gz"
	expected="$(printf '%s\n' "$checksums" |
		grep " ${archive_name}\$" |
		head -n 1 |
		awk '{ print $1 }')"

	if [ -z "$expected" ]; then
		error "No checksum entry for ${archive_name} in checksums.txt."
		return 1
	fi

	if [ "$expected" != "$actual" ]; then
		error "Checksum mismatch for ${archive_name}!"
		error "  expected: ${expected}"
		error "  actual:   ${actual}"
		error "Refusing to install a corrupted or tampered download."
		return 1
	fi

	completed "Checksum verified (sha256)."
}

# Extract only the binary. The release archives also carry README.md and
# LICENSE, and unpacking the whole archive into the bin directory would leave
# both of those in /usr/local/bin.
unpack() {
	archive=$1
	bin_dir=$2
	sudo=${3-}

	case "$archive" in
	*.tar.gz)
		flags=$(test -n "${VERBOSE-}" && echo "-xzvof" || echo "-xzof")
		${sudo} tar "${flags}" "${archive}" -C "${bin_dir}" "${BIN_NAME}"
		return 0
		;;
	*.zip)
		flags=$(test -z "${VERBOSE-}" && echo "-qqo" || echo "-o")
		UNZIP="${flags}" ${sudo} unzip "${archive}" "${BIN_NAME}.exe" -d "${bin_dir}"
		return 0
		;;
	esac

	error "Unknown package extension."
	printf "\n"
	info "This almost certainly results from a bug in this script--please file a"
	info "bug report at https://github.com/${GITHUB_REPO}/issues"
	return 1
}

usage() {
	printf "%s\n" \
		"install.sh [option]" \
		"" \
		"Fetch and install the latest version of ${BIN_NAME}, if ${BIN_NAME} is already" \
		"installed it will be updated to the latest version."

	printf "\n%s\n" "Options"
	printf "\t%s\n\t\t%s\n\n" \
		"-V, --verbose" "Enable verbose output for the installer" \
		"-f, -y, --force, --yes" "Skip the confirmation prompt during installation" \
		"--no-verify" "Skip sha256 checksum verification (not recommended)" \
		"-p, --platform" "Override the OS identified by the installer [default: ${PLATFORM}]" \
		"-b, --bin-dir" "Override the bin installation directory [default: ${BIN_DIR}]" \
		"-a, --arch" "Override the architecture identified by the installer [default: ${ARCH}]" \
		"-B, --base-url" "Override the base URL used for downloading releases [default: ${BASE_URL}]" \
		"-v, --version" "Install a specific version of ${BIN_NAME} (e.g. v0.1.0) [default: ${VERSION}]" \
		"-h, --help" "Display this help message"
}

elevate_priv() {
	if ! has sudo; then
		error 'Could not find the command "sudo", needed to get permissions for install.'
		info "If you are on Windows, please run your shell as an administrator, then"
		info "rerun this script. Otherwise, please run this script as root, or install"
		info "sudo."
		exit 1
	fi
	if ! sudo -v; then
		error "Superuser not granted, aborting installation"
		exit 1
	fi
}

install() {
	ext="$1"

	if test_writable "${BIN_DIR}"; then
		sudo=""
		msg="Installing ${BIN_NAME}, please wait..."
	else
		warn "Escalated permissions are required to install to ${BIN_DIR}"
		elevate_priv
		sudo="sudo"
		msg="Installing ${BIN_NAME} as root, please wait..."
	fi
	info "$msg"

	archive=$(get_tmpfile "$ext")

	# download to the temp file
	download "${archive}" "${URL}"

	# verify the download against the release checksums.txt
	verify_checksum "${archive}" "${ARCHIVE}"

	# unpack the binary into the bin dir, using sudo if required
	unpack "${archive}" "${BIN_DIR}" "${sudo}"

	rm -f "${archive}"
}

# Detect the GoReleaser GOOS value.
#   - darwin
#   - linux
#   - windows (Git Bash / MSYS / Cygwin)
detect_platform() {
	platform="$(uname -s | tr '[:upper:]' '[:lower:]')"

	case "${platform}" in
	msys_nt*) platform="windows" ;;
	cygwin_nt*) platform="windows" ;;
	mingw*) platform="windows" ;;
	darwin) platform="darwin" ;;
	linux) platform="linux" ;;
	esac

	printf '%s' "${platform}"
}

# Detect the GoReleaser GOARCH value.
#   - amd64
#   - arm64
#   - 386
#   - arm
detect_arch() {
	arch="$(uname -m | tr '[:upper:]' '[:lower:]')"

	case "${arch}" in
	x86_64) arch="amd64" ;;
	amd64) arch="amd64" ;;
	aarch64) arch="arm64" ;;
	arm64) arch="arm64" ;;
	i386 | i686) arch="386" ;;
	armv* | arm) arch="arm" ;;
	esac

	# `uname -m` in some cases mis-reports a 32-bit OS as 64-bit, double check.
	if [ "${arch}" = "amd64" ] && [ "$(getconf LONG_BIT 2>/dev/null || echo 64)" -eq 32 ]; then
		arch=386
	elif [ "${arch}" = "arm64" ] && [ "$(getconf LONG_BIT 2>/dev/null || echo 64)" -eq 32 ]; then
		arch=arm
	fi

	printf '%s' "${arch}"
}

confirm() {
	if [ -z "${FORCE-}" ]; then
		printf "%s " "${MAGENTA}?${NO_COLOR} $* ${BOLD}[y/N]${NO_COLOR}"
		set +e
		read -r yn </dev/tty
		rc=$?
		set -e
		if [ $rc -ne 0 ]; then
			error "Error reading from prompt (please re-run with the '--yes' option)"
			exit 1
		fi
		if [ "$yn" != "y" ] && [ "$yn" != "yes" ]; then
			error 'Aborting (please answer "yes" to continue)'
			exit 1
		fi
	fi
}

check_bin_dir() {
	bin_dir="${1%/}"

	if [ ! -d "$BIN_DIR" ]; then
		error "Installation location $BIN_DIR does not appear to be a directory"
		info "Make sure the location exists and is a directory, then try again."
		usage
		exit 1
	fi

	# https://stackoverflow.com/a/11655875
	good=$(
		IFS=:
		for path in $PATH; do
			if [ "${path%/}" = "${bin_dir}" ]; then
				printf 1
				break
			fi
		done
	)

	if [ "${good}" != "1" ]; then
		warn "Bin directory ${bin_dir} is not in your \$PATH"
	fi
}

is_build_available() {
	os="$1"
	arch="$2"
	target="$os/$arch"

	good=$(
		IFS=" "
		for t in $SUPPORTED_TARGETS; do
			if [ "${t}" = "${target}" ]; then
				printf 1
				break
			fi
		done
	)

	if [ "${good}" != "1" ]; then
		error "${arch} builds for ${os} are not yet available for ${BIN_NAME}"
		printf "\n" >&2
		info "If you would like to see a build for your configuration,"
		info "please create an issue requesting a build for ${MAGENTA}${target}${NO_COLOR}:"
		info "${BOLD}${UNDERLINE}https://github.com/${GITHUB_REPO}/issues/new${NO_COLOR}"
		printf "\n"
		exit 1
	fi
}

# defaults
if [ -z "${PLATFORM-}" ]; then
	PLATFORM="$(detect_platform)"
fi

if [ -z "${BIN_DIR-}" ]; then
	BIN_DIR=/usr/local/bin
fi

if [ -z "${ARCH-}" ]; then
	ARCH="$(detect_arch)"
fi

if [ -z "${BASE_URL-}" ]; then
	BASE_URL="https://github.com/${GITHUB_REPO}/releases"
fi

if [ -z "${VERSION-}" ]; then
	VERSION="latest"
fi

# Non-POSIX shells can break once executing code due to semantic differences
verify_shell_is_posix_or_exit

# parse argv variables
while [ "$#" -gt 0 ]; do
	case "$1" in
	-p | --platform)
		PLATFORM="$2"
		shift 2
		;;
	-b | --bin-dir)
		BIN_DIR="$2"
		shift 2
		;;
	-a | --arch)
		ARCH="$2"
		shift 2
		;;
	-B | --base-url)
		BASE_URL="$2"
		shift 2
		;;
	-v | --version)
		VERSION="$2"
		shift 2
		;;

	-V | --verbose)
		VERBOSE=1
		shift 1
		;;
	-f | -y | --force | --yes)
		FORCE=1
		shift 1
		;;
	--no-verify)
		NO_VERIFY=1
		shift 1
		;;
	-h | --help)
		usage
		exit
		;;

	-p=* | --platform=*)
		PLATFORM="${1#*=}"
		shift 1
		;;
	-b=* | --bin-dir=*)
		BIN_DIR="${1#*=}"
		shift 1
		;;
	-a=* | --arch=*)
		ARCH="${1#*=}"
		shift 1
		;;
	-B=* | --base-url=*)
		BASE_URL="${1#*=}"
		shift 1
		;;
	-v=* | --version=*)
		VERSION="${1#*=}"
		shift 1
		;;
	-V=* | --verbose=*)
		VERBOSE="${1#*=}"
		shift 1
		;;
	-f=* | -y=* | --force=* | --yes=*)
		FORCE="${1#*=}"
		shift 1
		;;

	*)
		error "Unknown option: $1"
		usage
		exit 1
		;;
	esac
done

TARGET="${PLATFORM}/${ARCH}"

is_build_available "${PLATFORM}" "${ARCH}"

# Resolve the release tag. GoReleaser strips a leading "v" from the version used
# in the archive filename, but the GitHub release tag keeps it. Track both.
if [ "${VERSION}" = "latest" ]; then
	info "Resolving latest release tag..."
	TAG="$(resolve_latest_tag)" || {
		error "Could not determine the latest release tag from GitHub."
		info "Specify a version explicitly with ${BOLD}--version vX.Y.Z${NO_COLOR}."
		exit 1
	}
else
	TAG="${VERSION}"
fi

# Strip a single leading "v" to build the archive filename version.
VER_NUM="${TAG#v}"

printf "  %s\n" "${UNDERLINE}Configuration${NO_COLOR}"
info "${BOLD}Bin directory${NO_COLOR}: ${GREEN}${BIN_DIR}${NO_COLOR}"
info "${BOLD}Platform${NO_COLOR}:      ${GREEN}${PLATFORM}${NO_COLOR}"
info "${BOLD}Arch${NO_COLOR}:          ${GREEN}${ARCH}${NO_COLOR}"
info "${BOLD}Version${NO_COLOR}:       ${GREEN}${TAG}${NO_COLOR}"

# non-empty VERBOSE enables verbose untarring
if [ -n "${VERBOSE-}" ]; then
	VERBOSE=v
	info "${BOLD}Verbose${NO_COLOR}: yes"
else
	VERBOSE=
fi

printf '\n'

EXT=tar.gz
if [ "${PLATFORM}" = "windows" ]; then
	EXT=zip
fi

# GoReleaser archive name template:
#   {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
ARCHIVE="${PROJECT_NAME}_${VER_NUM}_${PLATFORM}_${ARCH}.${EXT}"
URL="${BASE_URL}/download/${TAG}/${ARCHIVE}"

info "Tarball URL: ${UNDERLINE}${BLUE}${URL}${NO_COLOR}"
confirm "Install ${BIN_NAME} ${GREEN}${TAG}${NO_COLOR} to ${BOLD}${GREEN}${BIN_DIR}${NO_COLOR}?"
check_bin_dir "${BIN_DIR}"

install "${EXT}"

# Run what was installed, so a binary that is present but cannot start (the
# wrong architecture, a truncated file) is caught here rather than on first use.
installed="${BIN_DIR%/}/${BIN_NAME}"
if [ "${PLATFORM}" = "windows" ]; then
	installed="${installed}.exe"
fi
if reported="$("${installed}" --version 2>/dev/null)"; then
	completed "${reported} installed to ${BIN_DIR}"
else
	warn "${BIN_NAME} ${TAG} was installed to ${BIN_DIR}, but running it failed."
	warn "Try: ${installed} --version"
fi

printf '\n'
info "Run ${BOLD}${BIN_NAME} check openapi.yaml${NO_COLOR} to score a spec, or ${BOLD}${BIN_NAME} --help${NO_COLOR}."

if [ "${PLATFORM}" = "windows" ]; then
	printf '\n'
	info "On Windows, make sure ${BOLD}${BIN_DIR}${NO_COLOR} is on your PATH, or move"
	info "${BOLD}${BIN_NAME}.exe${NO_COLOR} to a directory that is."
fi

printf '\n'
