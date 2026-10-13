# Deploying FixMyDeshboard (free, no domain)

One Oracle Cloud "Always Free" VM in India runs the same Docker Compose setup as local
development: the app, Postgres and Caddy (automatic HTTPS). A free DuckDNS subdomain gives it
an address like `https://fixmydeshboard.duckdns.org`.

Takes about 30–45 minutes, most of it in Oracle's console.

## 1. Oracle Cloud account

1. Sign up at <https://www.oracle.com/cloud/free/>.
2. **Home region: pick India South (Hyderabad) or India West (Mumbai). This can't be changed
   later**, and Always Free machines only run in your home region.
3. A card is needed for verification. Oracle places a small temporary hold; Always Free
   resources are never charged.
4. Recommended, once the account is active: **Billing → Upgrade to Pay As You Go**. Oracle can
   reclaim Always Free VMs that look idle (and a small site mostly is); upgraded accounts are
   exempt. You still pay nothing while you stay within Always Free limits. Then set a budget
   alert (**Billing → Budgets**, e.g. ₹100) so you'd hear about any charge.

## 2. Create the VM

**Compute → Instances → Create instance**

- **Image:** Canonical Ubuntu 24.04.
- **Shape:** *Ampere* → `VM.Standard.A1.Flex`, 1 OCPU and 6 GB memory (labelled "Always Free-eligible").
  - If it says **out of capacity**: try another availability domain on the same page, try again
    later, or use `VM.Standard.E2.1.Micro` (AMD, 1 GB, also Always Free). The setup script adds
    swap so the small one works too.
- **Networking:** create a new virtual cloud network with a public subnet, and keep
  "Assign a public IPv4 address" on.
- **SSH keys:** upload your public key (`~/.ssh/id_ed25519.pub`, or `id_rsa.pub`).
- Create, and note the **public IP address** once it's running.

## 3. Open ports 80 and 443 in Oracle's network

**Networking → Virtual cloud networks →** your VCN **→ Security Lists → Default Security List →
Add Ingress Rules**, twice:

| Source CIDR | IP protocol | Destination port |
|---|---|---|
| `0.0.0.0/0` | TCP | `80` |
| `0.0.0.0/0` | TCP | `443` |

(The VM's own firewall also blocks these; the setup script opens that one.)

## 4. Free subdomain

1. Sign in at <https://www.duckdns.org> (GitHub or Google login).
2. Add the subdomain `fixmydeshboard` (or another free name).
3. Set its **current ip** to the VM's public IP and click **update ip**.

## 5. Run the setup script on the VM

```sh
ssh ubuntu@<public-ip>
curl -fsSL https://raw.githubusercontent.com/exitflynn/fix-my-deshboard/main/scripts/server-setup.sh -o setup.sh
DOMAIN=fixmydeshboard.duckdns.org bash setup.sh
```

After a minute, `https://fixmydeshboard.duckdns.org` should load. Login is off at this point;
anonymous reports and comments work.

## 6. Turn on login (can wait)

1. <https://console.cloud.google.com> → create a project, e.g. "FixMyDeshboard".
2. **APIs & Services → OAuth consent screen:** User type *External*; app name FixMyDeshboard;
   your email as support and developer contact. Add no scopes (the app only asks Google to
   confirm who you are). Then **Publish app** so anyone can log in, not only test users.
3. **Credentials → Create credentials → OAuth client ID:** type *Web application*;
   Authorized redirect URI `https://fixmydeshboard.duckdns.org/auth/callback`.
4. On the VM, put the client ID and secret into `~/fix-my-deshboard/.env`, then
   `cd ~/fix-my-deshboard && sudo docker compose up -d`.
5. Log in on the site, pick your username, add it to `ADMIN_USERS` in `.env`, and run
   `sudo docker compose up -d` again. You now have `/admin`.

## Updating

```sh
ssh ubuntu@<public-ip>
cd ~/fix-my-deshboard && git pull && sudo docker compose up -d --build
```

## Backups

The script schedules `scripts/backup.sh` nightly at 03:30: a database dump plus screenshots in
`~/fix-my-deshboard/backups`, kept 7 days. Those live on the same VM, so they don't survive
losing it. To copy them off the machine, install [rclone](https://rclone.org), configure a
remote (e.g. Backblaze B2, 10 GB free) and set `BACKUP_REMOTE` in `.env`.
