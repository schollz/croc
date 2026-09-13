#!/bin/sh
# Run only inside a disposable Debian/Ubuntu/Fedora/openSUSE container as root.
set -eu
artifacts=${1:?artifact directory required}
version=${2:?release version required}
version=${version#v}
artifacts=$(cd "$artifacts" && pwd)
export CROC_CONFIG_DIR=/tmp/croc-package-user-config
mkdir -p "$CROC_CONFIG_DIR"
printf 'preserve user configuration\n' > "$CROC_CONFIG_DIR/lifecycle-sentinel"

if command -v apt-get >/dev/null; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y ca-certificates
    arch=$(dpkg --print-architecture)
    package="$artifacts/croc_${version}-1_${arch}.deb"
    # A lower-revision fixture checks dpkg's upgrade path. It is not evidence
    # of compatibility with any third-party Debian package.
    previous=$(mktemp -d)
    dpkg-deb -R "$package" "$previous"
    sed -i "s/^Version:.*/Version: ${version}-0/" "$previous/DEBIAN/control"
    dpkg-deb --root-owner-group -b "$previous" /tmp/croc-previous.deb
    apt-get install -y /tmp/croc-previous.deb
    apt-get install -y "$package"
    test "$(dpkg-query -W -f='${Version}' croc)" = "${version}-1"
    dpkg-query -S /usr/bin/croc
elif command -v dnf >/dev/null; then
    arch=$(rpm --eval '%{_arch}')
    package="$artifacts/croc-${version}-1.${arch}.rpm"
    dnf install -y ca-certificates
    # Exercise the existing distro package when one is available.
    if dnf -q list --available croc >/dev/null 2>&1; then
        dnf install -y croc
        rpm -q croc
    else
        printf 'No repository croc available; historical upgrade not exercised\n'
    fi
    dnf install -y "$package"
    test "$(rpm -q --qf '%{VERSION}-%{RELEASE}' croc)" = "${version}-1"
    rpm -qf /usr/bin/croc
elif command -v zypper >/dev/null; then
    arch=$(rpm --eval '%{_arch}')
    package="$artifacts/croc-${version}-1.${arch}.rpm"
    zypper --non-interactive refresh
    zypper --non-interactive install ca-certificates
    # This is an upstream download without a distro signature; the fixture
    # accepts only that local package and leaves repository checks enabled.
    zypper --non-interactive --no-gpg-checks install "$package"
    test "$(rpm -q --qf '%{VERSION}-%{RELEASE}' croc)" = "${version}-1"
    rpm -qf /usr/bin/croc
else
    echo 'Unsupported lifecycle test image' >&2
    exit 1
fi

test "$(croc --version)" = "croc version $version"
croc --help >/dev/null
test ! -e "$CROC_CONFIG_DIR/install.json"
before=$(sha256sum /usr/bin/croc)
if croc update --register-installer; then
    echo 'Package registered for standalone updates' >&2
    exit 1
fi
test ! -e "$CROC_CONFIG_DIR/install.json"
printf '{"version":1,"method":"getcroc-installer","target":"/usr/bin/croc"}\n' > "$CROC_CONFIG_DIR/install.json"
croc update
test "$(sha256sum /usr/bin/croc)" = "$before"
if command -v apt-get >/dev/null; then
    apt-get remove -y croc
    apt-get install -y "$package"
    apt-get purge -y croc
elif command -v dnf >/dev/null; then
    dnf remove -y croc
    dnf install -y "$package"
    dnf remove -y croc
else
    zypper --non-interactive remove croc
    zypper --non-interactive --no-gpg-checks install "$package"
    zypper --non-interactive remove croc
fi
test ! -e /usr/bin/croc
test "$(cat "$CROC_CONFIG_DIR/lifecycle-sentinel")" = 'preserve user configuration'
printf 'Lifecycle and self-update protection verified\n'
