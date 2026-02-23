import hmac
import hashlib
import json
import time
import secrets as secrets_module
from urllib.parse import urlencode

import httpx
from fastapi import FastAPI, Request, HTTPException
from workers import WorkerEntrypoint

app = FastAPI()

SPOTIFY_AUTH_URL = "https://accounts.spotify.com/authorize"
SPOTIFY_TOKEN_URL = "https://accounts.spotify.com/api/token"
SPOTIFY_SCOPES = "playlist-modify-public playlist-modify-private"

# ---------------------------------------------------------
# Signing key — self-generated in KV on first use
# ---------------------------------------------------------
# NOTE: Bot-to-worker authentication is handled entirely by Cloudflare Zero Trust Access at the edge. 
# Requests reaching this Worker code have already been validated by CF Access. The only public
# endpoint is /callback (CF bypass policy), which is protected by HMAC state.

KV_SIGNING_KEY = "__signing_key__"
SPOTIFY_CLIENT_ID = "SPOTIFY_CLIENT_ID"
SPOTIFY_CLIENT_SECRET = "SPOTIFY_CLIENT_SECRET"
REDIRECT_URI = "REDIRECT_URI"
REFRESH_TOKEN = "refresh_token"


async def _get_signing_key(env) -> str:
    """
    Returns the CSRF signing key for OAuth state parameters.
    Self-generates on first use and persists the key in KV.
    This key only protects /callback (the single public endpoint) from CSRF.
    All other endpoints are protected by Cloudflare Access at the edge.
    """
    print("NOTICE: Getting signing key...")
    kv = env.SPOTIFY_TOKENS
    key = await kv.get(KV_SIGNING_KEY)
    if not key:
        print("NOTICE: No signing key found in KV. Generating first-time key.")
        key = secrets_module.token_hex(32)
        await kv.put(KV_SIGNING_KEY, key)
        print("SUCCESS: Signing key generated and persisted.")
    return key


def _require_env(name: str, env) -> str:
    value = getattr(env, name, None)
    if not value:
        print(f"CRITICAL: Environment variable '{name}' is missing from env bindings!")
        raise HTTPException(status_code=500, detail=f"Server misconfiguration: {name} not set")
    return value


async def _sign_state(user_id: str, env) -> str:
    """Return '{user_id}.{hmac_sha256(user_id, signing_key)}'."""
    key = await _get_signing_key(env)
    sig = hmac.new(key.encode(), user_id.encode(), hashlib.sha256).hexdigest()
    return f"{user_id}.{sig}"


async def _verify_state(state_param: str, env) -> tuple[bool, str]:
    """Verify HMAC-signed state. Returns (valid, user_id). Timing-safe."""
    try:
        user_id, signature = state_param.split(".", 1)
        key = await _get_signing_key(env)
        expected = hmac.new(key.encode(), user_id.encode(), hashlib.sha256).hexdigest()
        return hmac.compare_digest(signature, expected), user_id
    except HTTPException:
        print(f"CRITICAL: HTTPException when verifying state..")
        raise
    except Exception as e:
        print(f"CRITICAL: Exception when verifying state: {str(e)}")
        return False, ""


# ---------------------------------------------------------
# Token helpers
# ---------------------------------------------------------

def _add_expires_at(token_data: dict) -> dict:
    """Annotate token_data with an absolute expires_at Unix timestamp."""
    token_data["expires_at"] = int(time.time()) + token_data.get("expires_in", 3600)
    return token_data


def _needs_refresh(token_data: dict, buffer_seconds: int = 60) -> bool:
    return time.time() >= (token_data.get("expires_at", 0) - buffer_seconds)


async def _do_refresh(token_data: dict, env) -> dict:
    """
    Calls Spotify to refresh the access token. Merges into token_data,
    preserving refresh_token if Spotify doesn't issue a new one.
    Raises HTTPException(502) on Spotify error.
    """
    async with httpx.AsyncClient() as client:
        resp = await client.post(
            SPOTIFY_TOKEN_URL,
            data={
                "grant_type": REFRESH_TOKEN,
                REFRESH_TOKEN: token_data[REFRESH_TOKEN],
                "client_id": _require_env(SPOTIFY_CLIENT_ID, env),
                "client_secret": _require_env(SPOTIFY_CLIENT_SECRET, env),
            },
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )

    if resp.status_code != 200:
        raise HTTPException(
            status_code=502,
            detail=f"Spotify refresh failed ({resp.status_code}): {resp.text}",
        )

    new_data = resp.json()
    if REFRESH_TOKEN not in new_data:
        new_data[REFRESH_TOKEN] = token_data[REFRESH_TOKEN]
    token_data.update(new_data)
    return _add_expires_at(token_data)


# ---------------------------------------------------------
# Routes
# ---------------------------------------------------------

@app.get("/auth-url")
async def get_auth_url(user_id: str, request: Request):
    """
    Returns a signed Spotify OAuth URL for the given user_id.
    Protected by Cloudflare Access (service token) — no additional auth check needed here.

    The user_id becomes the KV storage key. Using the bot owner's Discord user ID
    here enables future per-user token expansion without API changes.
    """
    print(f"AUTH: Generating URL for user_id: {user_id}")
    if not user_id:
        print("ERROR: user_id missing from request")
        raise HTTPException(status_code=400, detail="user_id is required")

    try:
        state = await _sign_state(user_id, request.app.state.env)
        redirect_uri = _require_env(REDIRECT_URI, request.app.state.env)
        print(f"DEBUG: Using REDIRECT_URI: {redirect_uri}")

        params = {
            "client_id": _require_env(SPOTIFY_CLIENT_ID, request.app.state.env),
            "response_type": "code",
            "redirect_uri": redirect_uri,
            "scope": SPOTIFY_SCOPES,
            "state": state,
        }
        print(f"SUCCESS: Auth URL generated for {user_id}")
        return {"auth_url": f"{SPOTIFY_AUTH_URL}?{urlencode(params)}"}

    except Exception as e:
        print(f"ERROR: Failed to generate auth URL: {str(e)}")
        raise


@app.get("/callback")
async def spotify_callback(request: Request):
    """
    Spotify's OAuth redirect target. PUBLIC endpoint — CF Access bypass policy applies.
    Protected only by HMAC-signed state parameter (CSRF protection).

    Verifies state, exchanges authorization code for tokens, annotates with
    expires_at, and stores in KV keyed by user_id (extracted from state).
    """
    code = request.query_params.get("code")
    state = request.query_params.get("state")

    if not code or not state:
        print("ERROR: Spotify callback reached without code or state.")
        raise HTTPException(status_code=400, detail="Missing code or state")

    env = request.app.state.env

    valid, user_id = await _verify_state(state, env)
    if not valid:
        print(f"SECURITY: Invalid state parameter received. Potential CSRF or tampering. State: {state}")
        raise HTTPException(status_code=403, detail="Invalid or tampered state")

    print(f"AUTH: State verified. Exchanging code for user: {user_id}")

    async with httpx.AsyncClient() as client:
        resp = await client.post(
            SPOTIFY_TOKEN_URL,
            data={
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": _require_env(REDIRECT_URI, env),
                "client_id": _require_env(SPOTIFY_CLIENT_ID, env),
                "client_secret": _require_env(SPOTIFY_CLIENT_SECRET, env),
            },
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )

    if resp.status_code != 200:
        print(f"ERROR: Spotify token exchange failed. Status: {resp.status_code}, Body: {resp.text}")
        raise HTTPException(
            status_code=400,
            detail=f"Spotify token exchange failed ({resp.status_code}): {resp.text}",
        )

    token_data = _add_expires_at(resp.json())

    await env.SPOTIFY_TOKENS.put(user_id, json.dumps(token_data))

    print(f"SUCCESS: Tokens stored for user_id: {user_id}")
    return {"message": "Authentication successful. You can return to Discord."}


@app.get("/token/{user_id}")
async def get_token(user_id: str, request: Request):
    """
    Returns the stored token for user_id. Auto-refreshes if within 60s of expiry.
    Protected by Cloudflare Access (service token).
    """
    print(f"INFO: Getting token for user id: {user_id}")
    env = request.app.state.env
    stored_json = await env.SPOTIFY_TOKENS.get(user_id)
    if not stored_json:
        print(f"CRITICAL: No token available for user: {user_id}")
        raise HTTPException(status_code=404, detail="No token found for this user_id")

    token_data = json.loads(stored_json)

    if _needs_refresh(token_data):
        print(f"INFO: Token refresh needed for: {user_id}")
        token_data = await _do_refresh(token_data, env)
        await env.SPOTIFY_TOKENS.put(user_id, json.dumps(token_data))

    return token_data


@app.post("/refresh/{user_id}")
async def force_refresh(user_id: str, request: Request):
    """
    Force-refreshes the token for user_id regardless of expiry.
    Protected by Cloudflare Access (service token).
    """
    env = request.app.state.env
    stored_json = await env.SPOTIFY_TOKENS.get(user_id)
    if not stored_json:
        print(f"CRITICAL: No token available for user: {user_id}")
        raise HTTPException(status_code=404, detail="No token found for this user_id")

    token_data = await _do_refresh(json.loads(stored_json), env)
    await env.SPOTIFY_TOKENS.put(user_id, json.dumps(token_data))
    return token_data


class Default(WorkerEntrypoint):
    async def fetch(self, request):
        import asgi

        app.state.env = self.env
        return await asgi.fetch(app, request.js_object, self.env)
