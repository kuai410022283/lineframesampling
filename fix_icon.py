import os
try:
    from PIL import Image, ImageDraw
except ImportError:
    print("请先安装 Pillow: pip install Pillow")
    exit(1)

def make_corners_transparent(input_path, output_path, radius_percent=0.2, padding=1):
    """
    input_path: 原图标路径
    output_path: 输出路径
    radius_percent: 圆角比例
    padding: 向内收缩的像素，用于彻底切除边缘白边
    """
    if not os.path.exists(input_path):
        print(f"错误: 找不到文件 {input_path}")
        return

    img = Image.open(input_path)
    frames = []
    
    try:
        n_frames = img.n_frames
    except AttributeError:
        n_frames = 1
        
    print(f"正在处理 {input_path} (共 {n_frames} 个尺寸)...")
    
    for i in range(n_frames):
        img.seek(i)
        # 转换为带透明度的 RGBA
        frame = img.copy().convert("RGBA")
        width, height = frame.size
        
        # 1. 使用 4 倍超采样来获得平滑的边缘
        scale = 4
        sw, sh = width * scale, height * scale
        
        # 创建超大遮罩
        mask = Image.new("L", (sw, sh), 0)
        draw = ImageDraw.Draw(mask)
        
        # 计算圆角半径
        r = int(min(sw, sh) * radius_percent)
        
        # 计算内缩后的矩形范围（4倍缩放后的像素）
        p = padding * scale
        draw.rounded_rectangle((p, p, sw - p - 1, sh - p - 1), radius=r, fill=255)
        
        # 缩小遮罩回到原尺寸，使用高质量采样
        mask = mask.resize((width, height), resample=Image.LANCZOS)
        
        # 2. 将遮罩应用到 Alpha 通道
        frame.putalpha(mask)
        
        frames.append(frame)
        print(f"  尺寸 {width}x{height} 处理完成 (半径比例: {radius_percent}, 内缩: {padding}px)")

    # 保存为 ICO
    frames[0].save(output_path, format="ICO", append_images=frames[1:])
    print(f"成功！已保存到 {output_path}")

if __name__ == "__main__":
    # 增加半径到 0.15，并设置 1 像素内缩
    make_corners_transparent("app_icon.ico", "app_icon_transparent.ico", radius_percent=0.18, padding=1)
