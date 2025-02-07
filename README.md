# 🌒 DarkFlare

A stealthy TCP-over-CDN tunnel that keeps your connections cozy and comfortable behind Cloudflare's welcoming embrace.

## 🤔 What is this sorcery?

DarkFlare is a clever little tool that disguises your TCP traffic as innocent HTTPS requests, letting them pass through corporate firewalls like a VIP at a nightclub. It's like a tunnel, but with more style and less dirt.

It has two parts: a client-side proxy (darkflare-client) that encodes TCP data into HTTPS requests and sends it to a Cloudflare-protected domain, and a server-side proxy (darkflare-server) that decodes the requests and forwards the data to a local service (like SSH on port 22). It’s protocol-agnostic, secure, and uses Cloudflare's encrypted infrastructure, making it stealthy and scalable for accessing internal resources or bypassing network restrictions.

When using this remember the traffic over the tunnel is only as secure as the Cloudflare protection. Use your own encryption.

## 🧱 Why CDNs?
Services like Cloudflare, Akamai Technologies, Fastly, and Amazon CloudFront are not only widely accessible but also integral to the global internet infrastructure. In regions with restrictive networks, alternatives such as CDNetworks in Russia, ArvanCloud in Iran, or ChinaCache in China may serve as viable proxies. These CDNs support millions of websites across critical sectors, including government and healthcare, making them indispensable. Blocking them risks significant collateral damage, which inadvertently makes them reliable pathways for bypassing restrictions.

## ⛓️‍💥 Stop Network Censorship
Internet censorship is a significant issue in many countries, where governments restrict access to information by blocking websites and services. For instance, China employs the "Great Firewall" to block platforms like Facebook and Twitter, while Iran restricts access to social media and messaging apps. In Russia, authorities have intensified efforts to control information flow by blocking virtual private networks (VPNs) and other tools that citizens use to bypass censorship.

AP NEWS
 In such environments, a tool that tunnels TCP traffic over HTTP(S) through a Content Delivery Network (CDN) like Cloudflare can be invaluable. By disguising restricted traffic as regular web traffic, this method can effectively circumvent censorship measures, granting users access to blocked content and preserving the free flow of information.

```
                                FIREWALL/CENSORSHIP
                                |     |     |     |
                                v     v     v     v

[Client]──────┐                ┌──────────────────┐                ┌─────────[Target Service]
              │                │                  │                │       (e.g., SSH Server)
              │                │   CLOUDFLARE     │                │tcp      localhost:22
              │tcp             │     NETWORK      │                │
[darkflare    │                │                  │                │ [darkflare
 client]──────┼───HTTPS───────>│ (looks like      │─-HTTPS-───────>│  server]
localhost:2222│                │  normal traffic) │                │ :8080
              │                │                  │                │
              └────────────────┼──────────────────┼────────────────┘
                               │                  │
                               └──────────────────┘

Flow:
1. TCP traffic ──> darkflare-client
2. Wrapped as HTTPS ──> Cloudflare CDN
3. Forwarded to ──> darkflare-server
4. Unwrapped back to TCP ──> Target Service
```

## ⁇  Usecases
ssh, rdp, or anything tcp to bypass restrictive firewalls or state controled internet.

Tunneling ppp or other vpn services that leverage TCP.

darkflare-server can launch applications like sshd or pppd. Note that there are issues with host keys and certificate validation on sshd if you don't configure it properly.

Linux's popular pppd daemon will also not run as non-root in some cases, which would require a more complex configuration with sudo.

Breaking past blocked sites! 

[How to use NordVPN over TCP](https://support.nordvpn.com/hc/en-us/articles/19683394518161-OpenVPN-connection-on-NordVPN#:~:text=With%20NordVPN%2C%20you%20can%20connect,differences%20between%20TCP%20and%20UDP. "Configure NordVPN over TCP")

## NordVPN

1. Download the OpenVPN client 
2. Under Manual setup in your NordVPN web account download the .ovpn file for TCP
3. Also in Manual setup select username and password authentication.
4. Edit the .ovpn file and change the IP and port to your darkflare server IP and Port.
5. Configure darkflare-server to use the IP and port defined in the .ovpn file.
6. Import the .ovpn file to OpenVPN and setup your username and password.

I did provide an ./examples/nordvpn.ovpn for you to use. Also two scrips for up/down to fix some of the routing issues.

Speed tests were around 30Mbps down and 10Mbps up with latency around 30-100ms. Streaming, etc... all seem to work fine.

Note: OpenVPN does some weird thing with the default gateway/route. For testing purposes I added: pull-filter ignore "redirect-gateway" to the .ovpn file. That allows me to force the tunnel to not eat my network. 

![OpenVPN on NordVPN over TCPoCDN](https://raw.githubusercontent.com/doxx/darkflare/main/images/openvpn.jpg)

## 🔐 Few Obscureation Techniques

Requests are randomized to look like normal web traffic with jpg, php, etc... with random file names.

Client and server headers are set to look like normal web traffic. 

If you have other ideas please send them my way.


## 🌩️ Cloudflare Configuration 
Add your new proxy hostname into a free Cloudflare account.

Setup your origin rules to send that host to the origin server (darkflare-server) via the proxy port you choose. 

I used 8080 with a Cloudflare proxy via HTTP for the firs test. Less overhead.

## ✨ Features

- **Sneaky TCP Tunneling**: Wraps your TCP connections in a fashionable HTTPS outfit
- **Cloudflare Integration**: Because who doesn't want their traffic to look like it's just visiting Cloudflare?
- **Debug Mode**: For when things go wrong and you need to know why (spoiler: it's always DNS)
- **Session Management**: Keeps your connections organized like a Type A personality
- **TLS Security**: Because we're sneaky, not reckless

## 🚀 Quick Start

### Installation

1. Download the latest release from the releases page or binary from the main branch.
2. Extract the binaries to your preferred location
3. Make the binaries executable:
```bash
chmod +x darkflare-client darkflare-server
```

### Running the Client
```bash
./darkflare-client -l 2222 -t https://host.domain.net:443 
```
Add `-debug` flag for debug mode

### Notes
Make the host.domain.net you use is configured in Cloudflare to point to the darkflare-server. If you want to debug and go directly to the psudo server you can use the `-allow-direct` flag on the server.

### Running the Server

```bash
# HTTPS Server (recommended for production)
./darkflare-server -o https://0.0.0.0:443 -d localhost:22 -c /path/to/cert.pem -k /path/to/key.pem

# HTTP Server (for testing)
./darkflare-server -o http://0.0.0.0:8080 -d localhost:22
```

### Notes
- You must specify either `-d` (network destination) or `-a` (application) mode, but not both
- The `-allow-direct` flag allows direct connections without Cloudflare headers (not recommended for production)
- Debug mode (`-debug`) provides verbose logging of connections and data transfers
- Under SSL/TLS configuration in Cloudflare you need to set ssl encryption mode to Full.

### SSL/TLS Certificates

For HTTPS mode, you'll need to obtain origin certificates from Cloudflare:

1. Log into your Cloudflare dashboard
2. Go to SSL/TLS > Origin Server
3. Create a new certificate (or use an existing one)
4. Download both the certificate and private key
5. When starting the server in HTTPS mode, provide both certificate files:

Note: Keep your private key secure and never share it. The certificate provided by Cloudflare is specifically for securing the connection between Cloudflare and your origin server.

### Testing the Connection
```bash
ssh user@localhost -p 2222
```

## ⚠️ Security Considerations

- Always use end-to-end encryption for sensitive traffic
- The tunnel itself provides obscurity, not security
- Monitor your Cloudflare logs for suspicious activity
- Regularly update both client and server components

## ⚠️ Disclaimer

This tool is for educational purposes only. Please don't use it to bypass your company's firewall - your IT department has enough headaches already.

## 🤝 Contributing

Found a bug? Want to add a feature? PRs are welcome! Just remember:
- Keep it clean
- Keep it clever


## 📥 Downloads

### Latest Binary Downloads

#### Linux
- [darkflare-client-linux-amd64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-client-linux-amd64)
- [darkflare-server-linux-amd64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-server-linux-amd64)

#### macOS
- Intel (AMD64):
  - [darkflare-client-darwin-amd64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-client-darwin-amd64)
  - [darkflare-server-darwin-amd64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-server-darwin-amd64)
- Apple Silicon (ARM64):
  - [darkflare-client-darwin-arm64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-client-darwin-arm64)
  - [darkflare-server-darwin-arm64](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-server-darwin-arm64)

#### Windows
- [darkflare-client-windows-amd64.exe](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-client-windows-amd64.exe)
- [darkflare-server-windows-amd64.exe](https://raw.githubusercontent.com/blyon/darkflare/main/bin/darkflare-server-windows-amd64.exe)

### Verifying Binaries
```bash
# Download the checksums file
curl -O https://raw.githubusercontent.com/blyon/darkflare/main/bin/checksums.txt

# Verify downloads on Linux/macOS
shasum -a 256 -c checksums.txt

# Verify downloads on Windows PowerShell
Get-FileHash .\darkflare-client-windows-amd64.exe | Format-List
```

Note: All binaries are built automatically from the main branch. The checksums file is generated during the build process to ensure file integrity.

## 📜 License

MIT License - Because sharing is caring, but attribution is nice.

---
*Built with ❤️ and a healthy dose of mischief*

