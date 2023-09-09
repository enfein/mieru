Name: mieru
Version: 1.15.1
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
