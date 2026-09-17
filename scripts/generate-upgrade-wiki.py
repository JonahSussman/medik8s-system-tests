#!/usr/bin/env python3

import datetime
import sys


def generate() -> str:
    today = datetime.datetime.now(datetime.UTC).strftime("%Y-%m-%d %H:%M UTC")

    lines = [
        "# RHWA OpenShift 5 operator upgrade status",
        "",
        "This page is in flux until we nail down exactly what running upgrade tests looks like in the new Github Actions-based world.",
        "",
        f"Automatically refreshed: **{today}**",
    ]

    return "\n".join(lines)


if __name__ == "__main__":
    output = generate()
    
    if len(sys.argv) == 2:
        with open(sys.argv[1], "w", encoding="utf-8") as output_file:
            output_file.write(output)
    else:
        print(output, end="")
