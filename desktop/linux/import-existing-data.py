#!/usr/bin/env python3
"""Copy an existing Atlas database into a new, empty desktop profile."""

import os
import sqlite3
import sys
import tempfile


def open_readonly(path):
    return sqlite3.connect(f"file:{os.path.abspath(path)}?mode=ro", uri=True)


def record_count(connection):
    return sum(connection.execute(f"SELECT count(*) FROM {table}").fetchone()[0]
               for table in ("tasks", "reminders", "notes"))


def import_existing(source_path, target_path):
    if not os.path.isfile(source_path):
        return "No existing Atlas database to import."
    with open_readonly(source_path) as source:
        if source.execute("PRAGMA quick_check").fetchone()[0] != "ok":
            raise RuntimeError("Existing Atlas database failed its integrity check.")
        if record_count(source) == 0:
            return "Existing Atlas database has no records to import."
        source_version = source.execute("PRAGMA user_version").fetchone()[0]
        if os.path.exists(target_path):
            if os.path.exists(target_path + "-wal"):
                return "Desktop database has a write-ahead log; close Atlas before importing existing data."
            with open_readonly(target_path) as target:
                if record_count(target) != 0:
                    return "Desktop Atlas already has records; existing data was not overwritten."
                if target.execute("PRAGMA user_version").fetchone()[0] != source_version:
                    return "Desktop and existing database schemas differ; existing data was not imported."
        os.makedirs(os.path.dirname(target_path), mode=0o700, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix="atlas-import-", suffix=".db", dir=os.path.dirname(target_path))
        os.close(descriptor)
        try:
            with sqlite3.connect(temporary) as destination:
                source.backup(destination)
                if destination.execute("PRAGMA quick_check").fetchone()[0] != "ok":
                    raise RuntimeError("Imported Atlas database failed its integrity check.")
            os.chmod(temporary, 0o600)
            if os.path.exists(target_path):
                backup = target_path + ".before-import"
                if not os.path.exists(backup):
                    with open_readonly(target_path) as old, sqlite3.connect(backup) as saved:
                        old.backup(saved)
                    os.chmod(backup, 0o600)
            os.replace(temporary, target_path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    return "Imported existing tasks, reminders, and notes into desktop Atlas."


if __name__ == "__main__":
    try:
        print(import_existing(sys.argv[1], sys.argv[2]))
    except (OSError, sqlite3.Error, RuntimeError) as error:
        print(f"Atlas data import skipped: {error}", file=sys.stderr)
        sys.exit(1)
