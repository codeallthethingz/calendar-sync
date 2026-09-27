# calendar-sync

Copies events from several Google Calendars onto one hub calendar, so an
Appointment Schedule on the hub sees all busy time without a plan that checks
multiple calendars. Each copy keeps the title, description and location, gets a
per-source prefix and colour, and has no attendees, so nobody receives a second
invite.

A GitHub Action runs `sync` every 10 minutes and commits `state.json`, which holds
one sync token per source, keyed by source name. Calendar IDs never enter the
repo: they come from environment variables, which in Actions are secrets.

What gets copied, for the next 90 days:

- Declined invitations and events marked Free are skipped.
- All-day events are copied and marked Free on the hub.
- A calendar shared at free/busy level only gives no titles or details, so its
  events appear as `<prefix>busy`, for example `work: busy`.
- When a recurring event is edited, only instances inside the window are touched.

## Commands

| Command | What it does |
|---|---|
| `calendar-sync sync --config config.yaml --state state.json` | One sync pass over every source. Exits non-zero if any source failed. |
| `calendar-sync auth` | Opens a browser to sign in as the hub account and prints a refresh token. |
| `calendar-sync calendars` | Lists every calendar the hub account can see: id, name, access level. |

## Environment variables

| Variable | Used by |
|---|---|
| `GOOGLE_CLIENT_ID` | all commands |
| `GOOGLE_CLIENT_SECRET` | all commands |
| `GOOGLE_REFRESH_TOKEN` | `sync`, `calendars` |
| `HUB_CALENDAR_ID` | `sync` |
| `CALENDAR_ID_<NAME>` | `sync`, one per source in `config.yaml`, name upper-cased |

## Run locally

```sh
go build -o calendar-sync ./cmd/calendar-sync
export GOOGLE_CLIENT_ID=... GOOGLE_CLIENT_SECRET=... GOOGLE_REFRESH_TOKEN=...
./calendar-sync calendars
export HUB_CALENDAR_ID=... CALENDAR_ID_MASSAGE=... CALENDAR_ID_WILL=... CALENDAR_ID_LEGAL=... CALENDAR_ID_WORK=...
./calendar-sync sync --config config.yaml --state state.json
```

Tests: `go test ./...`

## Cut a release

```sh
git tag v0.2.0
git push origin v0.2.0
```

The release workflow builds `calendar-sync-linux-amd64` and attaches it to the
release. The sync workflow downloads the latest release when each chain link
starts.

## How the schedule works

GitHub's cron is best-effort and was observed firing a few times a day. So one
job loops for just under six hours, syncing every 10 minutes, then dispatches
its successor with the built-in token. The cron stays on as a backup that
restarts the chain if it stops. Cancel the running job to stop it; run the
workflow by hand to start it.

## One-time setup

1. Share each source calendar with the hub account at "See all event details".
2. In Google Cloud, signed in as the hub account: enable the Google Calendar API,
   set up the OAuth consent screen (Internal for a Workspace account, otherwise
   External and published), and create an OAuth client of type Desktop app.
3. Run `calendar-sync auth` with the client ID and secret set, and keep the
   refresh token it prints.
4. Run `calendar-sync calendars` to find the calendar ids.
5. Add the secrets: `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
   `GOOGLE_REFRESH_TOKEN`, `HUB_CALENDAR_ID` and one `CALENDAR_ID_<NAME>` per source.
6. Tag a release, then run the sync workflow by hand once and check the hub.
7. Create the Appointment Schedule on the hub's primary calendar.
