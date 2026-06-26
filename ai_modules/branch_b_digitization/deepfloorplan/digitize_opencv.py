import cv2
import numpy as np
import json
import os
import networkx as nx

def generate_mock_floorplan(filename="sample_blueprint.png"):
    print("Generating a mock floor plan image...")
    img = np.ones((500, 800, 3), dtype=np.uint8) * 255
    
    # Draw thick black borders
    cv2.rectangle(img, (50, 50), (750, 450), (0, 0, 0), 10)
    
    # Internal walls
    cv2.line(img, (300, 50), (300, 450), (0, 0, 0), 10) 
    cv2.line(img, (300, 250), (750, 250), (0, 0, 0), 10) 
    
    # Draw doors
    cv2.line(img, (300, 150), (300, 200), (255, 255, 255), 10)
    cv2.line(img, (500, 250), (550, 250), (255, 255, 255), 10)
    
    cv2.imwrite(filename, img)
    return filename

def rect_intersect(r1, r2, inflate=25):
    """
    Checks if two rectangles intersect after inflating them.
    If they do, it means the rooms are adjacent (sharing a wall/door).
    """
    x1, y1, w1, h1 = r1
    x2, y2, w2, h2 = r2
    
    ix1 = x1 - inflate
    iy1 = y1 - inflate
    iw1 = w1 + inflate*2
    ih1 = h1 + inflate*2
    
    ix2 = x2 - inflate
    iy2 = y2 - inflate
    iw2 = w2 + inflate*2
    ih2 = h2 + inflate*2
    
    return not (ix1 + iw1 < ix2 or ix2 + iw2 < ix1 or iy1 + ih1 < iy2 or iy2 + ih2 < iy1)

def _run_digitize_pass(original_img, kernel_size, min_area, inflate):
    img = original_img.copy()
    
    gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
    _, thresh = cv2.threshold(gray, 200, 255, cv2.THRESH_BINARY_INV)
    
    kernel = np.ones((kernel_size, kernel_size), np.uint8)
    closing = cv2.morphologyEx(thresh, cv2.MORPH_CLOSE, kernel)
    
    inverted = cv2.bitwise_not(closing)
    contours, hierarchy = cv2.findContours(inverted, cv2.RETR_LIST, cv2.CHAIN_APPROX_SIMPLE)
    
    zones = []
    zone_count = 1
    
    for cnt in contours:
        area = cv2.contourArea(cnt)
        if area > min_area:
            x, y, w, h = cv2.boundingRect(cnt)
            if w > img.shape[1] * 0.8 or h > img.shape[0] * 0.8:
                continue
                
            zone_name = f"Zone_{chr(64 + zone_count)}"
            zones.append({
                "name": zone_name,
                "bbox": (x, y, w, h),
                "area": area,
                "centroid": (int(x + w/2), int(y + h/2))
            })
            
            # Draw standard bounding boxes
            cv2.rectangle(img, (x, y), (x+w, y+h), (0, 255, 0), 2)
            cv2.putText(img, zone_name, (x+10, y+30), cv2.FONT_HERSHEY_SIMPLEX, 0.8, (0, 200, 0), 2)
            zone_count += 1
            
    print(f"Discovered {len(zones)} valid rooms (Nodes).")
    
    # --- PHASE 6: TOPOLOGIC DUAL GRAPH & SPACE SYNTAX ---
    print("Building Mathematical Topology (Dual Graph)...")
    G = nx.Graph()
    
    for z in zones:
        G.add_node(z["name"], area=z["area"], centroid=z["centroid"])
        
    edges_found = []
    for i in range(len(zones)):
        for j in range(i + 1, len(zones)):
            # Inflate boundaries by parameter to find adjacent rooms (edges)
            if rect_intersect(zones[i]["bbox"], zones[j]["bbox"], inflate=inflate):
                G.add_edge(zones[i]["name"], zones[j]["name"])
                edges_found.append((zones[i]["name"], zones[j]["name"]))
                
                # Draw the Dual Graph Edge (Connectivity)
                c1 = zones[i]["centroid"]
                c2 = zones[j]["centroid"]
                cv2.line(img, c1, c2, (0, 0, 255), 4) # Red topological line
                cv2.circle(img, c1, 10, (255, 0, 0), -1) # Blue nodes
                cv2.circle(img, c2, 10, (255, 0, 0), -1)
                
    # Calculate Space Syntax Integration (Closeness Centrality)
    closeness = nx.closeness_centrality(G)
    
    topology_data = {
        "nodes": [],
        "edges": edges_found,
        "space_syntax": {
            "most_integrated_room": max(closeness, key=closeness.get) if closeness else None
        }
    }
    
    for node, data in G.nodes(data=True):
        integration_score = round(closeness.get(node, 0), 3)
        topology_data["nodes"].append({
            "id": node,
            "integration_score": integration_score,
            "area": float(data["area"]),
            "centroid": data["centroid"]
        })
        # Annotate Space Syntax Score on the image
        cx, cy = data["centroid"]
        cv2.putText(img, f"Integ: {integration_score}", (cx - 40, cy + 25), 
                    cv2.FONT_HERSHEY_SIMPLEX, 0.7, (0, 0, 255), 2)
                    
    # Calculate Score
    num_zones = len(zones)
    score = 0
    if num_zones == 0:
        score = -1000
    elif num_zones > 80:
        score = -500 # Too much noise, shattered
    elif num_zones == 1:
        score = -100 # Over-blurred into one giant blob
    else:
        total_area = sum(z["area"] for z in zones)
        img_area = img.shape[0] * img.shape[1]
        coverage = total_area / img_area
        if coverage > 0.95:
            score -= 200 # Found the entire page border
        else:
            score += num_zones * 10
            score += int(coverage * 100)

    return score, img, topology_data

def auto_process_blueprint(image_path, progress_cb=None):
    img = cv2.imread(image_path)
    if img is None:
        return None, None

    kernels = [15, 25, 40, 60]
    areas = [1000, 3000, 6000]
    total_steps = len(kernels) * len(areas)
    
    best_score = -9999
    best_img = None
    best_data = None
    best_params = {}
    
    step = 0
    for k in kernels:
        for a in areas:
            step += 1
            if progress_cb:
                progress_cb(step, total_steps, f"Testing Kernel={k}, MinArea={a}...")
                
            score, img_res, data = _run_digitize_pass(img, k, a, 30)
            if score > best_score:
                best_score = score
                best_img = img_res
                best_data = data
                best_params = {"kernel": k, "area": a}
                
    if progress_cb:
        progress_cb(total_steps, total_steps, f"Auto-Selected Best Parameters: Kernel={best_params['kernel']}, Area={best_params['area']} (Score: {best_score})")
        
    return best_img, best_data

if __name__ == "__main__":
    test_img = "real_floorplan.png"
    if not os.path.exists(test_img):
        generate_mock_floorplan(test_img)
    auto_process_blueprint(test_img)

