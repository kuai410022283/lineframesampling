#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import argparse
import math
import os
import re
import sys
import tkinter as tk
from tkinter import filedialog

import pandas as pd


VERSION = "1.1.0"
REQUIRED_COLUMN = "ez_sys_geom"
CSV_ENCODINGS = ["utf-8-sig", "utf-8", "gb18030", "gbk"]


def get_distance_meters(lon1, lat1, lon2, lat2):
    """Calculate great-circle distance between two lon/lat points."""
    radius = 6371000
    phi1, phi2 = math.radians(lat1), math.radians(lat2)
    dphi = math.radians(lat2 - lat1)
    dlambda = math.radians(lon2 - lon1)
    a = math.sin(dphi / 2) ** 2 + math.cos(phi1) * math.cos(phi2) * math.sin(dlambda / 2) ** 2
    return 2 * radius * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def get_heading(p1, p2):
    """Return heading in radians using a local planar approximation."""
    lon1, lat1 = p1
    lon2, lat2 = p2
    avg_lat = math.radians((lat1 + lat2) / 2)
    dx = (lon2 - lon1) * math.cos(avg_lat)
    dy = lat2 - lat1
    return math.atan2(dy, dx)


def move_point(lon, lat, dx_meters, dy_meters):
    """Move a lon/lat point by dx/dy meters."""
    radius = 6371000
    new_lat = lat + (dy_meters / radius) * (180 / math.pi)
    new_lon = lon + (dx_meters / (radius * math.cos(math.radians(lat)))) * (180 / math.pi)
    return new_lon, new_lat


def points_to_wkt_polygon(points):
    if not points:
        return ""

    coords = list(points)
    if coords[0] != coords[-1]:
        coords.append(coords[0])

    coord_str = ", ".join(f"{lon:.8f} {lat:.8f}" for lon, lat in coords)
    return f"POLYGON (({coord_str}))"


def load_csv(file_path):
    last_error = None
    for encoding in CSV_ENCODINGS:
        try:
            return pd.read_csv(file_path, encoding=encoding)
        except UnicodeDecodeError as exc:
            last_error = exc
        except Exception as exc:
            last_error = exc
            break

    if last_error:
        print(f"Error: failed to read CSV '{file_path}': {last_error}")
    return None


def parse_wkt_linestring(wkt):
    if pd.isna(wkt) or not isinstance(wkt, str):
        return None

    try:
        match = re.search(r"\((.*)\)", wkt)
        if not match:
            return None

        coords = []
        for pair in match.group(1).split(","):
            parts = pair.strip().split()
            if len(parts) < 2:
                return None
            coords.append((float(parts[0]), float(parts[1])))
        return coords if len(coords) >= 2 else None
    except (TypeError, ValueError):
        return None


def build_shape_points(center_lon, center_lat, angle, size, shape):
    shape_points = []
    if shape == "circle":
        segments = 32
        for index in range(segments):
            theta = (2 * math.pi * index) / segments
            dx = size * math.cos(theta)
            dy = size * math.sin(theta)
            shape_points.append(move_point(center_lon, center_lat, dx, dy))
        return shape_points

    half = size / 2
    cos_a = math.cos(angle)
    sin_a = math.sin(angle)
    offsets = [(-half, -half), (half, -half), (half, half), (-half, half)]
    for ox, oy in offsets:
        rot_x = ox * cos_a - oy * sin_a
        rot_y = ox * sin_a + oy * cos_a
        shape_points.append(move_point(center_lon, center_lat, rot_x, rot_y))
    return shape_points


def process_csv(file_path, interval=100.0, size=100.0, shape="square"):
    if not os.path.exists(file_path):
        print(f"Error: file not found: {file_path}")
        return [], [], 0

    data_frame = load_csv(file_path)
    if data_frame is None:
        return [], [], 0

    if REQUIRED_COLUMN not in data_frame.columns:
        print(f"Error: required column '{REQUIRED_COLUMN}' not found. Columns: {list(data_frame.columns)}")
        return [], [], 0

    all_polylines = []
    all_results = []
    skipped_rows = 0

    for index, row in data_frame.iterrows():
        polyline = parse_wkt_linestring(row[REQUIRED_COLUMN])
        if not polyline:
            skipped_rows += 1
            print(f"Warning: skipped row {index} because '{REQUIRED_COLUMN}' is empty or invalid.")
            continue

        all_polylines.append(polyline)
        accumulated_total_dist = 0.0
        target_dist = 0.0

        for point_index in range(len(polyline) - 1):
            p1, p2 = polyline[point_index], polyline[point_index + 1]
            seg_len = get_distance_meters(p1[0], p1[1], p2[0], p2[1])
            if seg_len == 0:
                continue

            angle = get_heading(p1, p2)
            while accumulated_total_dist + seg_len >= target_dist:
                ratio = (target_dist - accumulated_total_dist) / seg_len
                center_lon = p1[0] + (p2[0] - p1[0]) * ratio
                center_lat = p1[1] + (p2[1] - p1[1]) * ratio
                shape_points = build_shape_points(center_lon, center_lat, angle, size, shape)

                all_results.append(
                    {
                        "line_id": index,
                        "dist": target_dist,
                        "center": (center_lon, center_lat),
                        "shape_points": shape_points,
                    }
                )
                target_dist += interval

            accumulated_total_dist += seg_len

    return all_polylines, all_results, skipped_rows


def run_visualization(polylines, results, line_color="#333333"):
    valid_polylines = [polyline for polyline in polylines if polyline and len(polyline) >= 2]
    if not valid_polylines:
        print("Visualization skipped: no valid polylines available.")
        return

    root = tk.Tk()
    root.title("Line Sampling Preview")

    canvas_width, canvas_height = 1000, 800
    canvas = tk.Canvas(root, width=canvas_width, height=canvas_height, bg="#121212")
    canvas.pack(padx=10, pady=10)

    all_points = [point for polyline in valid_polylines for point in polyline]
    lons = [point[0] for point in all_points]
    lats = [point[1] for point in all_points]
    min_lon, max_lon = min(lons), max(lons)
    min_lat, max_lat = min(lats), max(lats)

    def to_canvas(lon, lat):
        x = (lon - min_lon) / (max_lon - min_lon + 1e-9) * (canvas_width - 100) + 50
        y = (canvas_height - 100) - (lat - min_lat) / (max_lat - min_lat + 1e-9) * (canvas_height - 100) + 50
        return x, y

    for polyline in valid_polylines:
        for point_index in range(len(polyline) - 1):
            x1, y1 = to_canvas(*polyline[point_index])
            x2, y2 = to_canvas(*polyline[point_index + 1])
            canvas.create_line(x1, y1, x2, y2, fill=line_color, width=1)

    display_step = max(1, len(results) // 200)
    for result in results[::display_step]:
        center_x, center_y = to_canvas(*result["center"])
        canvas.create_oval(center_x - 1, center_y - 1, center_x + 1, center_y + 1, fill="#ff2e63")

        shape_canvas_points = [to_canvas(lon, lat) for lon, lat in result["shape_points"]]
        flat_points = [coord for pair in shape_canvas_points for coord in pair]
        canvas.create_polygon(flat_points, fill="", outline="#00adb5", width=1)

    print(f"Visualization started. Sample count: {len(results)}")
    root.mainloop()


def select_input_file():
    temp_root = tk.Tk()
    temp_root.withdraw()
    selected_file = filedialog.askopenfilename(
        title="Select input CSV file",
        filetypes=[("CSV Files", "*.csv"), ("All Files", "*.*")],
    )
    temp_root.destroy()
    return selected_file


def print_template():
    print("CSV template")
    print("=" * 60)
    print(f"Required column: {REQUIRED_COLUMN}")
    print("Format example:")
    print('name,ez_sys_geom,remark')
    print('"Demo line","LINESTRING(121.5000 38.9000, 121.6000 39.0000)","optional"')
    print("=" * 60)


def main():
    print("Line Frame Sampling Tool")
    print(f"Version: {VERSION}")
    print("-" * 60)

    parser = argparse.ArgumentParser(
        description="Sample points at a fixed interval from WKT LINESTRING values and export polygon frames."
    )
    parser.add_argument("input", nargs="?", help="Input CSV file path")
    parser.add_argument("-o", "--output", help="Output CSV file path")
    parser.add_argument("-i", "--interval", type=float, default=100.0, help="Sampling interval in meters")
    parser.add_argument("-s", "--size", type=float, default=100.0, help="Square edge length or circle radius in meters")
    parser.add_argument("-sh", "--shape", choices=["square", "circle"], default="square", help="Frame shape")
    parser.add_argument("--no-vis", action="store_true", help="Disable visualization")
    parser.add_argument("-lc", "--line-color", default="#333333", help="Line color for visualization")
    parser.add_argument("-t", "--template", action="store_true", help="Print CSV template and exit")
    args = parser.parse_args()

    if args.template:
        print_template()
        return 0

    is_gui_mode = False
    input_file = args.input
    if not input_file:
        input_file = select_input_file()
        is_gui_mode = True
        if not input_file:
            print("No file selected. Exit.")
            return 0

    if args.interval <= 0:
        print("Error: interval must be greater than 0.")
        return 1
    if args.size <= 0:
        print("Error: size must be greater than 0.")
        return 1

    if args.output:
        output_file = args.output
    else:
        base, ext = os.path.splitext(input_file)
        output_file = f"{base}_sampling_result{ext}"

    print(f"Processing: {input_file}")
    print(f"Config: shape={args.shape}, interval={args.interval}m, size={args.size}m")

    polylines, results, skipped_rows = process_csv(
        input_file,
        interval=args.interval,
        size=args.size,
        shape=args.shape,
    )

    if not results:
        print("No sampling result generated. Please check the input data.")
        if skipped_rows:
            print(f"Skipped invalid rows: {skipped_rows}")
        if is_gui_mode:
            input("\nPress Enter to exit...")
        return 1

    export_rows = []
    for result in results:
        export_rows.append(
            {
                "original_line_index": result["line_id"],
                "distance_m": round(result["dist"], 2),
                "center_lon": result["center"][0],
                "center_lat": result["center"][1],
                "geom_wkt": points_to_wkt_polygon(result["shape_points"]),
            }
        )

    pd.DataFrame(export_rows).to_csv(output_file, index=False, encoding="utf-8-sig")
    print(f"Done. Generated {len(results)} frames.")
    if skipped_rows:
        print(f"Skipped invalid rows: {skipped_rows}")
    print(f"Output: {output_file}")

    if not args.no_vis:
        run_visualization(polylines, results, line_color=args.line_color)

    if is_gui_mode:
        input("\nPress Enter to exit...")
    return 0


if __name__ == "__main__":
    sys.exit(main())
