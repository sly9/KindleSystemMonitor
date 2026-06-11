# Third-Party Notices

The `kindle-dash` binary (Go version under `go/`) bundles the following
third-party components.

---

## AMDFamily17.bin (embedded Pawn module)

Path in repo: `go/internal/metrics/pawn/AMDFamily17.bin`
Embedded via `go:embed` into the compiled `kindle-dash` binary.

This is a compiled Pawn module loaded into the [PawnIO](https://github.com/namazso/PawnIO)
kernel driver to read AMD CPU thermal-control SMN registers (0x59800
`F17H_M01H_THM_TCON_CUR_TMP`). It is used by `kindle-dash` to read CPU package
temperature on AMD Ryzen (Family 17h through 1Ah, Zen 1 through Zen 5) without
requiring an external monitoring process.

**Source:** Extracted from
[LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)
v0.9.4 (`LibreHardwareMonitorLib.dll` embedded resource
`LibreHardwareMonitor.Resources.PawnIo.AMDFamily17.bin`), which itself sourced
it from [PawnIO.Modules](https://github.com/namazso/PawnIO.Modules) release 0.1.6.

**License:** GNU Lesser General Public License v2.1 or later
([LGPL-2.1+](https://www.gnu.org/licenses/old-licenses/lgpl-2.1.txt)).
The source `.p` file is available at
<https://github.com/namazso/PawnIO.Modules/blob/main/AMDFamily17.p>.

**Copyright:** © 2023 namazso, contributors. All rights reserved by their owners.

Redistribution of this compiled module in the kindle-dash binary is permitted
under LGPL-2.1's terms governing distribution of "Combined Works" — the source
remains available at the URL above; users may relink against a modified
version by rebuilding from `internal/metrics/pawn/AMDFamily17.bin`.

---

## Source Han Sans CN Bold (embedded CJK font)

Path in repo: `go/internal/render/assets/font.otf`
Embedded via `go:embed` into the compiled `kindle-dash` binary.

Used to render the dashboard's labels and the CJK welcome/farewell screens.

**Source:** [Adobe Source Han Sans](https://github.com/adobe-fonts/source-han-sans),
`SubsetOTF/CN/SourceHanSansCN-Bold.otf`.

**License:** [SIL Open Font License 1.1](https://scripts.sil.org/cms/scripts/page.php?site_id=nrsi&id=OFL).
Embedding for any purpose, including commercial, is permitted.

**Copyright:** © 2014-2023 Adobe (<http://www.adobe.com/>), with Reserved Font
Name "Source". Source is a trademark of Adobe in the United States and/or
other countries.

---

## Go module dependencies

See `go/go.mod` for the exact set of transitive Go module dependencies and
their licenses. Notable ones:

| Module | Purpose | License |
|--------|---------|---------|
| `github.com/shirou/gopsutil/v4` | Native CPU/mem on Windows + macOS | BSD-3-Clause |
| `golang.org/x/crypto/ssh` | In-process SSH to Kindle | BSD-3-Clause |
| `github.com/Microsoft/go-winio` | Windows OpenSSH agent named pipe | MIT |
| `github.com/fogleman/gg` | 2D vector drawing | MIT |
| `golang.org/x/image/font/opentype` | OTF font parsing | BSD-3-Clause |

No nvidia-smi, LibreHardwareMonitor, or other external processes are required
or invoked at runtime — see `docs/部署-go版.md` for the dependency story.
