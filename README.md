# 💍 Oura Heart Rate ♥

A small CLI tool that fetches your current heart rate from the [Oura Ring API](https://cloud.ouraring.com/docs) and prints it to stdout.

```
♥ 62
```

## Setup

### 1. Register an Oura app

Go to [cloud.ouraring.com/oauth/applications](https://cloud.ouraring.com/oauth/applications) and create an app with:

- **Redirect URI:** `http://localhost:8085/callback`
- **Scopes:** `heartrate`

### 2. Set credentials

```sh
export OURA_CLIENT_ID="your-client-id"
export OURA_CLIENT_SECRET="your-client-secret"
```

### 3. Build

```sh
go build -o oura-hr .
```

### 4. Authorize

```sh
./oura-hr setup
```

Opens a browser for OAuth2 authorization. Tokens are saved to `~/.cache/oura-tokens.json` and refreshed automatically on expiry.

### 5. Run

```sh
./oura-hr
# ♥ 62
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `OURA_CLIENT_ID` | — | Required |
| `OURA_CLIENT_SECRET` | — | Required |
| `OURA_HR_CACHE_TTL` | `300` | Cache TTL in seconds |

Results are cached to `~/.cache/oura-hr` for the duration of the TTL to avoid unnecessary API calls.

## Terminal prompt integration

Works well as a [Starship](https://starship.rs) custom module:

```toml
[custom.oura_hr]
command = "/path/to/oura-hr"
when = "test -n \"$OURA_CLIENT_ID\""
format = "[$output]($style) "
style = "fg:#f7768e"
```
