#!/usr/bin/env bash
# One-time setup of a fresh Ubuntu server (e.g. Oracle Cloud Always Free) for FixMyDeshboard.
# Installs Docker, opens ports 80/443, clones the repo, writes .env, starts the site and
# schedules nightly backups. Safe to re-run: existing .env and data are kept.
#
# Usage, on the server:
#   curl -fsSL https://raw.githubusercontent.com/exitflynn/fix-my-deshboard/main/scripts/server-setup.sh -o setup.sh
#   DOMAIN=fixmydeshboard.duckdns.org bash setup.sh
set -euo pipefail

: "${DOMAIN:?Set DOMAIN first, e.g. DOMAIN=fixmydeshboard.duckdns.org bash setup.sh}"
REPO=https://github.com/exitflynn/fix-my-deshboard.git
DIR="$HOME/fix-my-deshboard"

echo "==> Swap (lets small VMs build the image)"
if [ -z "$(swapon --show)" ] && [ ! -e /swapfile ]; then
  sudo fallocate -l 2G /swapfile
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile >/dev/null
  sudo swapon /swapfile
  echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
fi

echo "==> Docker"
if ! command -v docker >/dev/null; then
  curl -fsSL https://get.docker.com | sudo sh
fi
sudo usermod -aG docker "$USER"

echo "==> Firewall: allow HTTP and HTTPS (Oracle's Ubuntu images reject them by default)"
for port in 80 443; do
  sudo iptables -C INPUT -p tcp --dport "$port" -j ACCEPT 2>/dev/null ||
    sudo iptables -I INPUT 1 -p tcp --dport "$port" -j ACCEPT
done
if command -v netfilter-persistent >/dev/null; then
  sudo netfilter-persistent save >/dev/null
fi

echo "==> Code"
if [ -d "$DIR/.git" ]; then
  git -C "$DIR" pull --ff-only
else
  git clone "$REPO" "$DIR"
fi
cd "$DIR"

echo "==> Config"
if [ ! -f .env ]; then
  umask 077
  cat > .env <<EOF
DOMAIN=$DOMAIN
BASE_URL=https://$DOMAIN
POSTGRES_PASSWORD=$(openssl rand -hex 24)

# Login. Leave empty to run with login off (anonymous posting still works).
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
# Your username, once you've logged in and picked one.
ADMIN_USERS=

DEV_LOGIN=
BACKUP_REMOTE=
EOF
  echo "    wrote $DIR/.env"
else
  echo "    keeping existing $DIR/.env"
fi

echo "==> Start"
sudo docker compose up -d --build

echo "==> Nightly backup at 03:30 (dumps kept 7 days in $DIR/backups)"
(crontab -l 2>/dev/null | grep -v 'scripts/backup.sh' || true
 echo "30 3 * * * $DIR/scripts/backup.sh >> $HOME/backup.log 2>&1") | crontab -

echo
echo "Done. Give HTTPS a minute to come up, then open https://$DOMAIN"
echo "To enable login later: edit $DIR/.env, then run: cd $DIR && sudo docker compose up -d"
