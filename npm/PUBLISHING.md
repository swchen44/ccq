# Publishing ccq to npm

ccq ships as a scoped package **`@swchen44/ccq`** using the esbuild model: one tiny launcher
package + five `os`/`cpu`-gated platform packages that each carry a prebuilt binary. npm installs
only the platform package that matches the user's machine — no postinstall download.

> Why scoped? `ccq` and `ccq-cli` are already taken on npm. `@swchen44/ccq` is free. The scope
> `@swchen44` must equal **your npm username** (a free "user scope"). If your chosen npm username
> differs, change `SCOPE=` in `build-npm.sh` and the names in `ccq/package.json` accordingly.

---

## One-time setup (you do this — it needs your credentials)

These steps create an account and log your machine in. **Claude cannot do these for you** (they
require entering a password / 2FA).

### 1. Create an npm account
- Go to **https://www.npmjs.com/signup**
- Pick **username = `swchen44`** (so the scope `@swchen44` is yours), enter email + password.
- Verify your email (npm sends a link).

### 2. Turn on 2FA (npm requires it to publish)
- npmjs.com → your avatar → **Account** → **Two-Factor Authentication** → enable (use an authenticator app).
- Choose "Authorization and Publishing" level.

### 3. Log your terminal in
```bash
npm login          # enter username, password, email; it opens a browser / asks for the 2FA code
npm whoami         # should print: swchen44
```

---

## Publish a release (each time)

### 4. Build the binaries + npm packages
```bash
cd ~/git/ccq
./build-release.sh v0.6.2        # produces dist/ccq-*-*.{tar.gz,zip}
npm/build-npm.sh   0.6.2         # produces npm/dist/<pkg>/ for all 6 packages
```
(Use your real version; it must be valid semver, e.g. `0.6.2` — not `0.6.2-test`.)

### 5. Publish — platform packages FIRST, then the launcher
Scoped packages default to *private* (paid). `--access public` makes them free + public.
```bash
cd ~/git/ccq/npm/dist
for p in ccq-darwin-x64 ccq-darwin-arm64 ccq-linux-x64 ccq-linux-arm64 ccq-win32-x64; do
  ( cd "$p" && npm publish --access public )
done
( cd ccq && npm publish --access public )    # the launcher last (its optionalDeps now exist)
```
The first publish of a brand-new package name may prompt for your 2FA code.

### 6. Verify
```bash
npm view @swchen44/ccq version          # should show 0.6.2
npm i -g @swchen44/ccq                   # installs launcher + your platform binary only
ccq version                              # -> ccq 0.6.2
# or without installing:  npx @swchen44/ccq version
```

> Reminder: npm delivers only the ccq binary. Users still need **clangd** on PATH (or `--clangd`).

---

## Later: automate it in CI (optional)

1. Create an **Automation** token: npmjs.com → avatar → **Access Tokens** → **Generate New Token**
   → *Automation* (bypasses 2FA in CI). Copy it.
2. GitHub repo → **Settings → Secrets and variables → Actions → New repository secret**:
   name `NPM_TOKEN`, value = the token.
3. Add a publish job to `.github/workflows/release.yml` that runs after the binaries build:
   ```yaml
   - run: npm/build-npm.sh "${GITHUB_REF_NAME#v}"
   - run: |
       echo "//registry.npmjs.org/:_authToken=${NPM_TOKEN}" > ~/.npmrc
       cd npm/dist
       for p in ccq-darwin-x64 ccq-darwin-arm64 ccq-linux-x64 ccq-linux-arm64 ccq-win32-x64 ccq; do
         ( cd "$p" && npm publish --access public ) || true
       done
     env: { NPM_TOKEN: "${{ secrets.NPM_TOKEN }}" }
   ```
   Then every `git tag vX.Y.Z && git push --tags` publishes to npm too.

## Troubleshooting
- **402 Payment Required** → you forgot `--access public` on a scoped package.
- **403 Forbidden / name** → the scope isn't yours; ensure `npm whoami` == the scope (`swchen44`).
- **EOTP / one-time pass** → enter your 2FA code (or use an Automation token in CI).
- **User installs but `ccq` not found** → ensure npm global bin is on PATH (`npm bin -g`).
