# Licensing

Effective 2026-10-30, PicPilot's original code owned by suakitsu is available
under the [MIT License](LICENSE). This grant covers only material for which
the maintainer controls copyright; it does not relicense third-party code,
data, assets, or their derivatives. Existing third-party notices take priority
for the material they identify.

## Included AGPL material

| Material | License | Source and modification record |
| --- | --- | --- |
| `third_party/nova-image-studio/`, including the modified embedded frontend | AGPL-3.0 | [Upstream and pinned version](third_party/nova-image-studio/UPSTREAM.md), [license](third_party/nova-image-studio/LICENSE) |
| `third_party/infinite-canvas/`, including the modified embedded frontend | AGPL-3.0 | [Provenance and modifications](third_party/infinite-canvas/PROVENANCE.txt), [license](third_party/infinite-canvas/LICENSE) |
| `core/nova/defaults/prompts.json` and `core/nova/defaults/blacklist.json` | AGPL-3.0 | [Nova source-data notice](core/nova/defaults/NOTICE.md) |

The third-party source, license texts, copyright notices and provenance remain
in the repository. PicPilot's modifications to the bundled AGPL components
remain under AGPL-3.0. The original AGPL text is also retained in
[LICENSES/AGPL-3.0.txt](LICENSES/AGPL-3.0.txt).

## Combined application

The current application includes modified AGPL canvases and embeds Nova's
AGPL-covered default data in its Go backend. The combined application is
distributed under AGPL-3.0; PicPilot-owned original portions are additionally
available under MIT. The full application is therefore not MIT-only.

Distributing or operating the AGPL-covered combined application must preserve
the applicable AGPL notices and corresponding-source obligations, including
the source offer for remote network users of a modified version. Corresponding
source must match the version actually deployed, including modifications and
the necessary build and installation scripts. Publish further modifications
when the AGPL requires it. A root MIT file or an automatically detected GitHub
license label does not override these obligations.

The MIT grant permits separate reuse of PicPilot-owned original code. It does
not authorize taking AGPL material or a covered combined work under MIT alone.

## Other dependencies

Go and npm dependencies retain their own licenses. Their package manifests,
lockfiles and distributed notices identify those dependencies. Preserve the
required notices when redistributing dependency code or compiled bundles.

## Release history

Both GitHub repositories were authorized for public publication by the owner
on 2026-10-30. Earlier commits retain their recorded license notices; this
change does not rewrite history or withdraw permissions previously granted.
