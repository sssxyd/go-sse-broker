Name:		sse-broker
Version:	1.0.7
Release:	1%{?dist}
Summary:	SSE Broker Service

Group:	        Networking/Daemons	
License:	Apache 2
URL:		https://github.com/sssxyd/go-sse-broker
Source0:	%{name}-%{version}.tar.gz

#BuildRequires:	
Requires:	systemd

%description
A powerful and flexible SSE (Server-Sent Events) server designed to offer a complete SSE service solution

%prep
%setup -q

%build
%global _build_id_links none

%install
mkdir -p %{buildroot}/usr/local/bin
mkdir -p %{buildroot}/etc/sse-broker
mkdir -p %{buildroot}/var/log/sse-broker
mkdir -p %{buildroot}/usr/lib/systemd/system

install -m 0755 sse-broker %{buildroot}/usr/local/bin/sse-broker
install -m 0644 sse-broker.service %{buildroot}/usr/lib/systemd/system/sse-broker.service
install -m 0644 config.toml %{buildroot}/etc/sse-broker/config.toml

%files
/usr/local/bin/sse-broker
/usr/lib/systemd/system/sse-broker.service
%config(noreplace) /etc/sse-broker/config.toml
/var/log/sse-broker/

%post
systemctl daemon-reload
systemctl enable sse-broker
echo "Please edit /etc/sse-broker/config.toml, then systemctl start sse-broker"

%preun
if [ $1 -eq 0 ]; then
    systemctl stop sse-broker
    systemctl disable sse-broker
fi

%postun
if [ $1 -eq 0 ]; then
    rm -rf /var/log/sse-broker/
    rm -f /etc/sse-broker/config.toml
fi
