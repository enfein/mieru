# The Go release binaries are already stripped and don't contain debug info.
%global debug_package %{nil}
%global __strip /bin/true

Name: mieru
Version: 3.40.0
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


%files
%{_bindir}/%{name}
