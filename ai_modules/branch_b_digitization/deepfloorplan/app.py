import streamlit as st
import cv2
import tempfile
import os
from digitize_opencv import auto_process_blueprint

st.set_page_config(layout="wide", page_title="DeepFloorplan Scanner")

st.title("🏗️ DeepFloorplan: Autonomous Scanner")
st.markdown("Upload any 2D architectural blueprint (`.png`, `.jpg`). The Autonomous AI will automatically search for the best computer vision parameters to trace the walls, define the HVAC zones, and build a mathematical Dual Graph of the Space Syntax.")

uploaded_file = st.file_uploader("Upload Blueprint Image", type=["png", "jpg", "jpeg"])

if uploaded_file is not None:
    # Save the uploaded file to a temporary location so OpenCV can read it
    with tempfile.NamedTemporaryFile(delete=False, suffix=".png") as tmp:
        tmp.write(uploaded_file.getvalue())
        tmp_path = tmp.name

    with st.spinner("Running AI Hyperparameter Simulations..."):
        progress_bar = st.progress(0, text="Initializing Grid Search...")
        
        def update_progress(current, total, msg):
            pct = int((current / total) * 100)
            progress_bar.progress(pct, text=msg)

        # Run the autonomous OpenCV pipeline
        img_result, topology_data = auto_process_blueprint(tmp_path, progress_cb=update_progress)
    
    # Clean up the temp file
    os.remove(tmp_path)

    if img_result is not None:
        st.success("Analysis Complete!")
        
        col1, col2 = st.columns([2, 1])
        
        with col1:
            st.subheader("Dual Graph Topology")
            # OpenCV BGR -> RGB for Streamlit
            img_rgb = cv2.cvtColor(img_result, cv2.COLOR_BGR2RGB)
            st.image(img_rgb, use_container_width=True)
            
        with col2:
            st.subheader("Space Syntax Data")
            if topology_data and "space_syntax" in topology_data:
                most_integ = topology_data["space_syntax"].get("most_integrated_room")
                st.info(f"**Most Integrated Room (Core):** {most_integ}")
            
            st.json(topology_data)
    else:
        st.error("Failed to process the image.")
