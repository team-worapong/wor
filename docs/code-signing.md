# Code signing (Windows)

How official `wor.exe` builds get an Authenticode signature, why this
particular route was chosen, and what still has to be done by hand.

## Why signing is mandatory, not optional polish

Windows 11's Smart App Control blocks unsigned executables before the
process is created. An unsigned `wor.exe` fails with:

```
Program 'wor.exe' failed to run: An Application Control policy has blocked this file
```

Nothing inside wor's own code can work around this — the binary never
runs. Smart App Control is on by default on clean Windows 11 installs,
has no per-app allow list, and until late 2025 could not even be switched
back on after being switched off. For Windows distribution, signing is the
only real fix.

Smart App Control's documented pass condition is a certificate issued by
any CA in the Microsoft Trusted Root Program. There is no documented
OV/EV distinction for it.

## Why SignPath Foundation

Three options were evaluated in August 2026.

**Azure Artifact Signing (formerly Trusted Signing) — ruled out on
geography.** USD 9.99/month, no hardware token, clean CI integration, and
it would otherwise be the obvious choice. But its Public Trust
certificates are only available to organizations in the US, Canada, the
EU, the UK, Australia, New Zealand, Japan, South Korea, Singapore,
Switzerland, Norway and Israel, and to individual developers in the US and
Canada only. Thailand is on neither list and there is no exception
process. Billing also starts at account creation and is not pro-rated, so
an attempt that fails identity validation still costs a full month.

**EV certificate — ruled out on value.** Microsoft's current documentation
states that EV certificates stopped bypassing SmartScreen in 2024 and that
"paying a premium for EV solely to avoid SmartScreen warnings is no longer
justified". DigiCert says the same. Since June 2023 the CA/Browser Forum
has also required OV private keys to sit on FIPS-certified hardware, the
rule that used to be EV's differentiator, so the two tiers are now
operationally near-identical.

**SignPath Foundation — chosen.** Free for open-source projects, and the
option Microsoft's own code signing guidance recommends. wor-host
qualifies: Apache 2.0 (an OSI-approved licence with no commercial
dual-licensing), actively maintained, already released in the form to be
signed, not a hacking tool.

The trade-off: the certificate is issued to SignPath Foundation, so
Windows shows **SignPath Foundation** as the publisher rather than
Worapong Sriwichian. In exchange the project inherits an established
publisher identity instead of starting from zero reputation.

Fallback if SignPath ever declines or withdraws: an OV certificate from
Sectigo/Comodo, roughly USD 220–290/year, plus a hardware token or a cloud
HSM service. Budget for the CI friction — a USB token cannot be plugged
into a GitHub-hosted runner.

## What is already in the repository

| Piece | Where | What it does |
|---|---|---|
| Version resource source | `winres/winres.json` | ProductName, CompanyName, copyright and so on for the Windows binary |
| Resource generation | `scripts/build.sh` (`make_windows_resource`) | Runs `go-winres` before the windows target and stamps the version from `internal/version/version.go` |
| Package-without-rebuild | `scripts/release.sh --skip-build` | Lets a signing step sit between building and packaging |
| Signing instructions | `.signpath/artifact-configurations/default.xml` | Tells SignPath which file in the uploaded archive to sign, and what metadata it must carry |
| Release pipeline | `.github/workflows/release.yml` | Build → sign → package → publish |
| Public policy page | `public/code-signing/index.php` | Required by SignPath Foundation's terms |

### Why the version resource matters

Go emits no Win32 VERSIONINFO resource by default, so a plain `go build`
produces an `.exe` with an empty ProductName and ProductVersion. SignPath
Foundation requires every signed binary to carry enforced product-name and
product-version metadata, and the artifact configuration checks both — so
without `go-winres` the signing request is rejected outright.

`scripts/build.sh` treats a missing `go-winres` as a hard error rather than
a warning, matching how `wor service add` blocks when a template's runtime
is absent. A build that quietly produced an unsignable binary would only
fail much later, in CI, after a human had already approved the request.

Install it locally with:

```bash
go install github.com/tc-hib/go-winres@latest
```

and make sure `$(go env GOPATH)/bin` is on your PATH. It is only needed
for the windows target; `./scripts/build.sh linux amd64` and the macOS
targets never invoke it.

## Manual setup — the parts no script can do

### 1. Apply to SignPath Foundation

<https://signpath.org/apply.html>

The form asks for the repository URL, the licence, the download page, and
a description. The reviewer is checking two things: that the binary really
originates from this project, and that the build is reproducible and
resistant to tampering. Suggested answers:

- **Project / repository:** <https://github.com/team-worapong/wor>
- **Licence:** Apache License 2.0 (OSI-approved, no commercial
  dual-licensing)
- **Download page:** <https://wor.worapong.com/download>
- **What it is:** a cross-platform command-line host manager for Linux,
  macOS and Windows. It deploys and supervises Node.js, Go, Python, PHP
  and static web services, generates nginx/Apache virtual hosts, manages
  SSL certificates and database backups, and diagnoses services that stop
  serving — all under one filesystem convention.
- **Artifact to sign:** a single Go-built Windows executable,
  `wor-windows-amd64.exe`, shipped inside the release `.zip` / `.tar.gz`.
  No installer, no DLLs, no bundled third-party binaries.
- **Build:** GitHub Actions on GitHub-hosted runners, from a tagged
  commit; see `.github/workflows/release.yml`. Go standard library only —
  no third-party runtime dependencies.
- **Why signing is needed:** Windows Smart App Control blocks the unsigned
  binary before it runs, so Windows users currently cannot use the tool at
  all.

Turnaround is not officially published; third-party reports suggest one to
two weeks. Do not block a release on it — keep shipping unsigned and
backfill signing once approved. (Their terms in fact require the project to
already be released in the form to be signed.)

### 2. Publish the code signing policy

`public/code-signing/index.php` is written and linked from the footer of
the home page, the docs page and the download page. Deploy it and confirm
<https://wor.worapong.com/code-signing/> resolves before the review.

Two pieces of its wording are prescribed by SignPath and must not be
reworded:

- the attribution — *"Free code signing provided by SignPath.io,
  certificate by SignPath Foundation"*
- the privacy sentence — *"This program will not transfer any information
  to other networked systems unless specifically requested by the user or
  the person installing or operating it"*

Also required, and not something a file in this repo can satisfy:
**multi-factor authentication on both the SignPath account and the GitHub
account**, for every person listed in the policy's roles.

### 3. Configure the SignPath side

Once approved, in the SignPath web UI:

1. Note the **organization ID** — click the organization name, upper right.
2. Create the **project** with slug `wor-host`, and set its repository URL
   to `https://github.com/team-worapong/wor`. Origin verification checks
   against this.
3. Add the predefined **"GitHub.com" trusted build system** to the
   organization, then link it to the project.
4. Upload `.signpath/artifact-configurations/default.xml` as an artifact
   configuration with slug `default`.
5. Confirm the signing policy slug is `release-signing`.
6. Generate an **API token**: your username, upper right → My profile →
   API Token → Generate token. It is displayed once only. Prefer a
   dedicated CI user over a personal account.

Then in the GitHub repository settings:

- secret `SIGNPATH_API_TOKEN`
- variable `SIGNPATH_ORGANIZATION_ID`

If any of those slugs differ from the values above, change them in
`.github/workflows/release.yml` to match — they are not discovered
automatically.

### 4. Cut a release

Push a tag matching `v*`. The workflow builds, uploads the unsigned
binary, and waits. Approve the signing request in SignPath; the workflow
then downloads the signed binary, packages it and publishes the release.

The approval wait is why `wait-for-completion-timeout-in-seconds` is set
to 10800 (three hours) rather than the action's 600-second default.

### 5. Verify before announcing

On a Windows machine:

```powershell
Get-AuthenticodeSignature .\wor.exe | Format-List Status, SignerCertificate, TimeStamperCertificate
```

`Status` must be `Valid`, and `TimeStamperCertificate` must not be empty —
a signature without a timestamp becomes invalid the moment the certificate
expires, retroactively breaking every release already shipped.

Then test on a machine with Smart App Control actually enabled, which is
the only test that answers the original question.

## Known limits

- **Only `wor-windows-amd64.exe` is signed.** The Linux and macOS binaries
  are unsigned. macOS notarization is a separate problem with a separate
  cost and is not addressed here; macOS users may see Gatekeeper warnings.
- **Signing cannot be done locally.** SignPath Foundation only signs
  artifacts it can prove were built by a GitHub-hosted runner from this
  repository, verified by reading the workflow run metadata from GitHub's
  API rather than trusting the build script. `scripts/build.sh` and
  `scripts/release.sh` still work on a laptop; they just produce unsigned
  binaries.
- **A signature is not instant reputation.** There are 2026 reports of
  correctly signed binaries still being blocked by Smart App Control for
  weeks, with no authoritative answer from Microsoft. SmartScreen
  reputation separately builds per file hash over download volume and can
  take weeks. Do not promise users the block disappears the moment the
  first signed release ships.
- **Keep the same signing identity.** Changing certificates resets the
  publisher signal and starts reputation over.
