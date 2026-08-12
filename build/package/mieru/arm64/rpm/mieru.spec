Name: mieru
Version: 3.35.0
Release: 1%{?dist}
Summary: Mieru proxy client
License: GPLv3+
URL: https://github.com/enfein/mieru


%description
Mieru proxy client.


%prep


%build


%install
mkdir -p %{buildroot}%{_bindir}
install -m 0755 %{name} %{buildroot}%{_bindir}/%{name}


%post
################################################################################
# Developer note: sync %post with build/package/mieru/arm64/debian/DEBIAN/postinst
################################################################################
set -e

# Grant CAP_NET_ADMIN so that the TUN interface can be created without root.
# This is best-effort: libcap may not be installed on all systems, and file
# capabilities may not be supported on all filesystems.
if [ -x /sbin/setcap ] || [ -x /usr/sbin/setcap ]; then
    setcap cap_net_admin+ep %{_bindir}/%{name} || true
fi


%files
%{_bindir}/%{name}
