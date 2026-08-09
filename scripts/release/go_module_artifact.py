#!/usr/bin/env python3
"""Prepare and deploy immutable Go module release artifacts."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any


SEMVER_RE = re.compile(r"^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:[-+][0-9A-Za-z.-]+)?$")
DESCRIPTOR_PATH = "payloads/release-assets/go-module.json"
ARCHIVE_PATH = "payloads/release-assets/go-module-source.tar.gz"


class ArtifactError(Exception):
    pass


def run(
    command: list[str],
    *,
    cwd: Path,
    env: dict[str, str] | None = None,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        command,
        cwd=cwd,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if check and result.returncode != 0:
        details = result.stderr.strip() or result.stdout.strip()
        raise ArtifactError(f"command failed: {' '.join(command)}: {details}")
    return result


def require_command(command: str) -> None:
    if shutil.which(command) is None:
        raise ArtifactError(f"required command is missing: {command}")


def repository_root() -> Path:
    require_command("git")
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        raise ArtifactError("current directory is not a Git repository")
    return Path(result.stdout.strip()).resolve()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_json(path: Path, subject: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ArtifactError(f"{subject} is missing or invalid: {path}") from exc
    if not isinstance(value, dict):
        raise ArtifactError(f"{subject} must be a JSON object: {path}")
    return value


def prepare(_: argparse.Namespace) -> int:
    version = os.environ.get("RELEASE_VERSION", "")
    artifact_dir_value = os.environ.get("RELEASE_ARTIFACT_DIR", "")
    if not SEMVER_RE.fullmatch(version):
        raise ArtifactError("Go module releases require a v-prefixed SemVer RELEASE_VERSION")
    if not artifact_dir_value:
        raise ArtifactError("RELEASE_ARTIFACT_DIR is required")

    go_command = os.environ.get("GO_COMMAND", "go")
    require_command(go_command)
    root = repository_root()
    go_mod_path = root / "go.mod"
    if not go_mod_path.is_file():
        raise ArtifactError("go.mod is required at the repository root")

    artifact_dir = Path(artifact_dir_value).resolve()
    staging = load_json(artifact_dir / "staging.json", "release staging area")
    if staging.get("artifact_kind") != "mprlab.release.staging":
        raise ArtifactError("release staging area has an invalid contract")
    if staging.get("version") != version:
        raise ArtifactError("release staging version does not match RELEASE_VERSION")
    source_commit = str(staging.get("source_commit") or "")
    head_commit = run(["git", "rev-parse", "HEAD"], cwd=root).stdout.strip()
    if source_commit != head_commit:
        raise ArtifactError("HEAD changed after release staging was initialized")

    module_path = run([go_command, "list", "-m", "-f", "{{.Path}}"], cwd=root).stdout.strip()
    if not module_path or any(character.isspace() for character in module_path):
        raise ArtifactError("Go module path is invalid")
    packages = sorted(filter(None, run([go_command, "list", "./..."], cwd=root).stdout.splitlines()))
    if not packages:
        raise ArtifactError("Go module contains no packages")

    archive = artifact_dir / ARCHIVE_PATH
    descriptor_path = artifact_dir / DESCRIPTOR_PATH
    archive.parent.mkdir(parents=True, exist_ok=True)
    archive.unlink(missing_ok=True)
    descriptor_path.unlink(missing_ok=True)
    archive_root = f"go-module-{version}/"
    run(
        [
            "git",
            "archive",
            "--format=tar.gz",
            f"--prefix={archive_root}",
            f"--output={archive}",
            source_commit,
        ],
        cwd=root,
    )

    descriptor = {
        "schema_version": 1,
        "artifact_kind": "mprlab.go-module",
        "module_path": module_path,
        "version": version,
        "source_commit": source_commit,
        "go_mod_sha256": sha256_file(go_mod_path),
        "packages": packages,
        "source_archive": {
            "path": ARCHIVE_PATH,
            "sha256": sha256_file(archive),
            "root": archive_root,
        },
    }
    descriptor_path.write_text(json.dumps(descriptor, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Prepared {module_path}@{version} source artifact.")
    return 0


def validate_published(manifest_path: Path, descriptor_path: Path, version: str) -> dict[str, str]:
    manifest = load_json(manifest_path, "published release manifest")
    descriptor = load_json(descriptor_path, "published Go module descriptor")
    if manifest.get("schema_version") != 2 or manifest.get("artifact_kind") != "mprlab.release":
        raise ArtifactError("published release manifest has an invalid contract")
    if manifest.get("version") != version:
        raise ArtifactError("published release manifest has the wrong version")
    if descriptor.get("schema_version") != 1 or descriptor.get("artifact_kind") != "mprlab.go-module":
        raise ArtifactError("published Go module descriptor has an invalid contract")
    if descriptor.get("version") != version:
        raise ArtifactError("published Go module descriptor has the wrong version")
    if descriptor.get("source_commit") != manifest.get("source_commit"):
        raise ArtifactError("published Go module source commit does not match the release manifest")

    payloads = {
        entry.get("path"): entry
        for entry in manifest.get("payloads", [])
        if isinstance(entry, dict) and isinstance(entry.get("path"), str)
    }
    descriptor_entry = payloads.get(DESCRIPTOR_PATH)
    archive_entry = payloads.get(ARCHIVE_PATH)
    if descriptor_entry is None or archive_entry is None:
        raise ArtifactError("published release has no complete Go module payload; run make release and make publish")
    if descriptor_entry.get("sha256") != sha256_file(descriptor_path):
        raise ArtifactError("published Go module descriptor does not match make release")
    source_archive = descriptor.get("source_archive")
    if not isinstance(source_archive, dict) or source_archive.get("path") != ARCHIVE_PATH:
        raise ArtifactError("published Go module source archive path is invalid")
    if source_archive.get("sha256") != archive_entry.get("sha256"):
        raise ArtifactError("published Go module source archive hash is inconsistent")

    values = {
        "module_path": descriptor.get("module_path"),
        "release_commit": manifest.get("release_commit"),
        "source_commit": manifest.get("source_commit"),
        "go_mod_sha256": descriptor.get("go_mod_sha256"),
    }
    if any(not isinstance(value, str) or not value or "\n" in value for value in values.values()):
        raise ArtifactError("published Go module release metadata is incomplete")
    return values


def exact_remote_tag(root: Path, remote: str, version: str) -> str:
    peeled = run(
        ["git", "ls-remote", "--tags", remote, f"refs/tags/{version}^{{}}"],
        cwd=root,
    ).stdout.strip()
    if peeled:
        return peeled.split()[0]
    direct = run(["git", "ls-remote", "--tags", remote, f"refs/tags/{version}"], cwd=root).stdout.strip()
    return direct.split()[0] if direct else ""


def make_writable(path: Path) -> None:
    if not path.exists():
        return
    for current_root, directories, files in os.walk(path, topdown=False):
        for name in files:
            file_path = Path(current_root) / name
            file_path.chmod(file_path.stat().st_mode | stat.S_IWUSR)
        for name in directories:
            directory_path = Path(current_root) / name
            directory_path.chmod(directory_path.stat().st_mode | stat.S_IWUSR)
    path.chmod(path.stat().st_mode | stat.S_IWUSR)


def selected_version(root: Path, requested: str | None) -> str:
    if requested:
        return requested
    tags = run(
        ["git", "tag", "--points-at", "HEAD", "--list", "v*", "--sort=-version:refname"],
        cwd=root,
    ).stdout.splitlines()
    if not tags:
        raise ArtifactError("no exact release tag at HEAD; pass --version after make publish")
    return tags[0]


def deploy(args: argparse.Namespace) -> int:
    if not args.proxy or "," in args.proxy or "|" in args.proxy:
        raise ArtifactError("--proxy must identify one proxy without fallback separators")
    gh_command = os.environ.get("GH_COMMAND", "gh")
    go_command = os.environ.get("GO_COMMAND", "go")
    require_command(gh_command)
    require_command(go_command)
    root = repository_root()
    version = selected_version(root, args.version)
    if not SEMVER_RE.fullmatch(version):
        raise ArtifactError("Go module deploy requires a v-prefixed SemVer version")

    temporary_directory = Path(tempfile.mkdtemp(prefix="mprlab-go-module-deploy-"))
    try:
        download_directory = temporary_directory / "download"
        module_cache = temporary_directory / "module-cache"
        download_directory.mkdir()
        module_cache.mkdir()
        try:
            run(
                [
                    gh_command,
                    "release",
                    "download",
                    version,
                    "--pattern",
                    "manifest.json",
                    "--pattern",
                    "go-module.json",
                    "--dir",
                    str(download_directory),
                ],
                cwd=root,
            )
        except ArtifactError as exc:
            raise ArtifactError(
                f"published Go module assets are unavailable; run make publish before deploy: {exc}"
            ) from exc
        release = validate_published(
            download_directory / "manifest.json",
            download_directory / "go-module.json",
            version,
        )
        remote_tag_commit = exact_remote_tag(root, args.remote, version)
        if remote_tag_commit != release["release_commit"]:
            raise ArtifactError(f"published release manifest does not match remote tag {version}")

        module_spec = f"{release['module_path']}@{version}"
        if args.dry_run:
            print("deploy_dry_run=true")
            print(f"module={module_spec}")
            print(f"proxy={args.proxy}")
            print(f"release_commit={release['release_commit']}")
            print(f"source_commit={release['source_commit']}")
            return 0

        download_env = os.environ.copy()
        download_env.update(
            {
                "GOMODCACHE": str(module_cache),
                "GOPROXY": args.proxy,
                "GONOPROXY": "",
                "GOPRIVATE": "",
                "GONOSUMDB": "",
                "GOSUMDB": os.environ.get("GO_MODULE_SUMDB", "sum.golang.org"),
                "GOTOOLCHAIN": "local",
            }
        )
        downloaded = run(
            [go_command, "mod", "download", "-json", module_spec],
            cwd=root,
            env=download_env,
            check=False,
        )
        if downloaded.returncode != 0:
            details = downloaded.stderr.strip() or downloaded.stdout.strip()
            raise ArtifactError(f"Go module proxy did not provide {module_spec}: {details}")
        try:
            download = json.loads(downloaded.stdout)
        except json.JSONDecodeError as exc:
            raise ArtifactError("Go module proxy returned invalid download metadata") from exc
        if download.get("Path") != release["module_path"] or download.get("Version") != version:
            raise ArtifactError("Go module proxy returned the wrong module identity")
        origin = download.get("Origin") or {}
        if origin.get("VCS") != "git" or origin.get("Hash") != release["release_commit"]:
            raise ArtifactError("Go module proxy origin does not match the published release commit")
        if not download.get("Sum") or not download.get("GoModSum"):
            raise ArtifactError("Go module proxy response is missing module checksums")
        downloaded_go_mod = Path(str(download.get("GoMod") or ""))
        if not downloaded_go_mod.is_file():
            raise ArtifactError("Go module proxy response is missing the downloaded go.mod")
        if sha256_file(downloaded_go_mod) != release["go_mod_sha256"]:
            raise ArtifactError("Go module proxy go.mod does not match make release")

        print(
            json.dumps(
                {
                    "module": module_spec,
                    "origin_commit": release["release_commit"],
                    "sum": download["Sum"],
                    "go_mod_sum": download["GoModSum"],
                },
                indent=2,
                sort_keys=True,
            )
        )
        print(f"Deployed {module_spec} to {args.proxy}.")
        return 0
    finally:
        make_writable(temporary_directory)
        shutil.rmtree(temporary_directory, ignore_errors=True)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    prepare_parser = subparsers.add_parser("prepare", help="Prepare the local Go module payload.")
    prepare_parser.set_defaults(func=prepare)
    deploy_parser = subparsers.add_parser("deploy", help="Activate a published version through a Go module proxy.")
    deploy_parser.add_argument("--remote", default="origin")
    deploy_parser.add_argument("--proxy", default="https://proxy.golang.org")
    deploy_parser.add_argument("--version")
    deploy_parser.add_argument("--dry-run", action="store_true")
    deploy_parser.set_defaults(func=deploy)
    return parser


def main() -> int:
    args = build_parser().parse_args()
    try:
        return args.func(args)
    except ArtifactError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
