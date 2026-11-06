"""Create a cross-built tarball with usable Unix executable permissions.

Build-time utility only; the distributed translation server needs no Python.
"""
import pathlib
import sys
import tarfile

bundle = pathlib.Path(sys.argv[1]).resolve(strict=True)
destination = pathlib.Path(sys.argv[2]).resolve()
if not bundle.is_dir() or destination == bundle:
    raise SystemExit("invalid package paths")

def permissions(info):
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    info.mode = 0o755 if info.isdir() or pathlib.PurePosixPath(info.name).name in {"jptranslate", "gamel10n"} else 0o644
    return info

with tarfile.open(destination, "w:gz", format=tarfile.PAX_FORMAT) as archive:
    archive.add(bundle, arcname=bundle.name, filter=permissions)
