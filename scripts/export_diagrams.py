#!/usr/bin/env python3
import os
import re
import subprocess

DIAGRAM_NAMES = [
    "01_requirements_mindmap",
    "02_high_level_architecture",
    "03_request_lifecycle",
    "04_api_gateway_flow",
    "05_auth_passwordless_flow",
    "06_posts_creation_flow",
    "07_interactions_like_flow",
    "08_assets_presigned_flow",
    "09_search_inverted_index",
    "10_database_er_diagram",
    "11_data_interaction_overview",
]

def main():
    root_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
    sys_design_path = os.path.join(root_dir, "docs", "system-design.md")
    out_dir = os.path.join(root_dir, "assets", "diagrams")
    os.makedirs(out_dir, exist_ok=True)

    with open(sys_design_path, "r", encoding="utf-8") as f:
        content = f.read()

    # Find all mermaid code blocks: ```mermaid ... ```
    pattern = re.compile(r"```mermaid\n(.*?)```", re.DOTALL)
    matches = pattern.findall(content)

    print(f"Found {len(matches)} mermaid diagrams in {sys_design_path}")
    if len(matches) != len(DIAGRAM_NAMES):
        print(f"Warning: expected {len(DIAGRAM_NAMES)} diagrams, got {len(matches)}")

    for i, mermaid_src in enumerate(matches):
        name = DIAGRAM_NAMES[i] if i < len(DIAGRAM_NAMES) else f"diagram_{i+1:02d}"
        mmd_file = f"{name}.mmd"
        svg_file = f"{name}.svg"
        png_file = f"{name}.png"

        mmd_path = os.path.join(out_dir, mmd_file)
        with open(mmd_path, "w", encoding="utf-8") as f:
            f.write(mermaid_src.strip() + "\n")

        print(f"[{i+1}/{len(matches)}] Rendering {name}...")

        # Render SVG
        res_svg = subprocess.run([
            "docker", "run", "--rm",
            "-u", f"{os.getuid()}:{os.getgid()}",
            "-v", f"{out_dir}:/data",
            "minlag/mermaid-cli:latest",
            "-i", f"/data/{mmd_file}",
            "-o", f"/data/{svg_file}"
        ], capture_output=True, text=True)

        if res_svg.returncode != 0:
            print(f"  Error rendering SVG for {name}: {res_svg.stderr}")
        else:
            print(f"  ✓ {svg_file}")

        # Render PNG (with 2x scale for high resolution)
        res_png = subprocess.run([
            "docker", "run", "--rm",
            "-u", f"{os.getuid()}:{os.getgid()}",
            "-v", f"{out_dir}:/data",
            "minlag/mermaid-cli:latest",
            "-i", f"/data/{mmd_file}",
            "-o", f"/data/{png_file}",
            "-s", "2"
        ], capture_output=True, text=True)

        if res_png.returncode != 0:
            print(f"  Error rendering PNG for {name}: {res_png.stderr}")
        else:
            print(f"  ✓ {png_file}")

    print("\nAll Mermaid diagrams exported to /assets/diagrams successfully.")

if __name__ == "__main__":
    main()
