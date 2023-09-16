Name: mita
Version: 2.0.0+alpha.1
Release: 1%{?dist}
Summary: Mieru proxy server
License: GPLv3+
URL: https://github.com/enfein/mieru


# To set systemd unit vendor preset to enabled, we need following dependency:
#
# BuildRequires: systemd-rpm-macros
#
# To make this rpm package easy to build (e.g. in a debian distribution), we
# decided to ignore all those build dependencies.


%description
Mieru proxy server.


%prep


%build


%install
mkdir -p %{buildroot}%{_bindir}
mkdir -p %{buildroot}/lib/systemd/system
install -m 0755 %{name} %{buildroot}%{_bindir}/%{name}
install %{name}.service %{buildroot}/lib/systemd/system/%{name}.service


# The execution sequence of upgrade rpm package:
# 1. %pre
# 2. %post
# 3. %preun
# 4. %postun
# 5. %posttrans


%pre


%post
################################################################################
# Developer note: sync %post with build/package/mita/amd64/debian/DEBIAN/postinst
################################################################################
/usr/bin/id mita > /dev/null 2>&1
rc=$?
if [ $rc -ne 0 ]; then
    /usr/sbin/useradd --no-create-home --user-group mita
fi

set -e

mkdir -p /etc/mita
chmod 775 /etc/mita

systemctl daemon-reload

# Server daemon will run with the system.
systemctl enable mita.service
systemctl start mita.service


%preun
################################################################################
# Developer note: sync %preun with build/package/mita/amd64/debian/DEBIAN/prerm
################################################################################
set -e

systemctl stop mita.service


%postun
################################################################################
# Developer note: sync %postun with build/package/mita/amd64/debian/DEBIAN/postrm
################################################################################
set -e

systemctl daemon-reload


%posttrans
set -e

systemctl start mita.service


%files
%{_bindir}/%{name}
/lib/systemd/system/%{name}.service
