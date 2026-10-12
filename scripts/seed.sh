#!/bin/sh
# Fills a local dev instance with example issues. Needs DEV_LOGIN=1 and ADMIN_USERS=admin1.
# Usage: scripts/seed.sh [base-url]   (default http://localhost:8080)
set -eu
B=${1:-http://localhost:8080}
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
login() { # login <jar> <dev-name> <username>
  curl -sf -c "$tmp/$1" -b "$tmp/$1" -o /dev/null -d "name=$2" "$B/login/dev"
  curl -s -b "$tmp/$1" -o /dev/null -d "username=$3" "$B/welcome"
}
issue() { curl -sf -o /dev/null "$@" "$B/issues"; }

login user seed rail_watcher
login admin admin admin1

issue -b "$tmp/user" -F 'title=Cancelled, rescheduled and diverted train lists "temporarily" unavailable since 2023' \
  -F 'site=https://enquiry.indianrail.gov.in/mntes/' -F seen_at=2026-10-12T09:30 \
  -F 'site_said=Due to some technical activity this site will be un-available for some time' \
  -F 'body=All three lists show the same notice. The Internet Archive has the **same notice in September 2023**.'
issue -F 'title=Calendar lets you search dates past the 60-day booking window' -F 'site=IRCTC Rail Connect app' \
  -F seen_at=2026-10-11T21:15 -F 'site_said=Reservation not allowed... (80012)' \
  -F 'body=Search works for any date; only opening a class shows the error, with **Book Now** still next to it.'
issue -F 'title=District-wise data still uses names and divisions changed three years ago' \
  -F 'site=https://rajasthan.gov.in/' -F seen_at=2026-10-10T14:00 -F 'site_said=Districts: Jaipur, Jodhpur, Ajmer (old list)'
issue -b "$tmp/user" -F 'title=Appointment availability table never finishes loading' \
  -F 'site=https://www.passportindia.gov.in/' -F seen_at=2026-10-09T11:40 -F 'site_said=Loading…'
issue -F 'title=PNR status page shows a blank table for valid PNRs' -F 'site=https://www.indianrail.gov.in/enquiry/PNR' \
  -F seen_at=2026-10-08T08:10 -F 'site_said=(empty table, no error)'

curl -s -o /dev/null -d 'body=Still the same today from Lucknow, on mobile and desktop.' "$B/issues/1/comments"
curl -s -b "$tmp/user" -o /dev/null -d 'body=Same for me. The **Book Now** button makes it look bookable.' "$B/issues/2/comments"

admin() { curl -s -b "$tmp/admin" -o /dev/null "$@"; }
admin -d action=save --data-urlencode 'title=Cancelled, rescheduled and diverted train lists "temporarily" unavailable since 2023' \
  -d 'labels=accepted, outage' -d service=NTES "$B/admin/issues/1"
admin -d action=save -d 'title=Calendar lets you search dates past the 60-day booking window' \
  -d 'labels=misleading-ui, triage' -d service=IRCTC "$B/admin/issues/2"
admin -d action=save -d 'title=District-wise data still uses names and divisions changed three years ago' \
  -d 'labels=stale-data, triage' -d 'service=Rajasthan Data Portal' "$B/admin/issues/3"
admin -d action=save -d 'title=PNR status page shows a blank table for valid PNRs' \
  -d 'labels=broken-table, triage' -d service=NTES "$B/admin/issues/5"
admin -d action=comment --data-urlencode 'body=Thanks. Confirmed on desktop; marking this verified.' "$B/admin/issues/1"
admin -d action=close --data-urlencode 'body=The table loads again as of 12 Oct. Reopen if you see it again.' "$B/admin/issues/4"
echo "Seeded $B"
