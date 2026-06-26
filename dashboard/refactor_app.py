import re

with open('src/App.jsx', 'r') as f:
    content = f.read()

# 1. Add import for useDigitalTwin
content = content.replace("import { SimState } from './telemetry';", "import { SimState } from './telemetry';\nimport { useDigitalTwin } from './useDigitalTwin';")

# 2. Remove getInitialSimData and INTEGRATION_BY_TYPE
content = re.sub(r"const INTEGRATION_BY_TYPE = \{.*?\n\};\n\n\n\nconst getInitialSimData = \(\) => \{.*?\n\};\n", "", content, flags=re.DOTALL)

# 3. Replace the top of App() up to the end of the WebSocket useEffect
target_pattern = r"function App\(\) \{.*?(?=  useEffect\(\(\) => \{\n    logEndRef\.current\?\.scrollIntoView)"
replacement = """function App() {
  const [activeFloor, setActiveFloor] = useState(6);
  const [selectedZone, setSelectedZone] = useState(null);
  const [showAiModal, setShowAiModal] = useState(false);
  const [panelSize, setPanelSize] = useState({ w: 600, h: 400 });
  const [activeLeftTab, setActiveLeftTab] = useState('ai');
  const [isLeftPanelOpen, setIsLeftPanelOpen] = useState(true);
  const [leftPanelSize, setLeftPanelSize] = useState({ w: 360 });
  const [showWindSim, setShowWindSim] = useState(true);
  const [windPanelSize, setWindPanelSize] = useState({ w: 400, h: 300 });
  const [maintenanceTarget, setMaintenanceTarget] = useState(null);
  const [ontology, setOntology] = useState(null);
  const [viewMode, setViewMode] = useState('hybrid');
  const ontologyRef = useRef(null);

  const [nodes, setNodes] = useState([]);
  const [edges, setEdges] = useState([]);
  const [liveLogs, setLiveLogs] = useState([]);
  const logEndRef = useRef(null);
  const activeFloorRef = useRef(activeFloor);

  useEffect(() => {
    fetch('http://localhost:8080/api/ontology')
      .then(res => res.json())
      .then(data => { setOntology(data); ontologyRef.current = data; })
      .catch(err => console.error("Failed to load Brick ontology:", err));
  }, []);

  const onSimUpdate = useCallback((newSimData, currentScenario) => {
    setNodes(nds => nds.map(n => {
      if (n.type === 'zone' && newSimData.zones[n.id]) {
          return { ...n, data: { ...n.data, temp: newSimData.zones[n.id].temp.toFixed(1), alert: newSimData.zones[n.id].alert } };
      }
      if (n.type === 'vav' && newSimData.vavs[n.id]) {
          return { ...n, data: { ...n.data, flow: newSimData.vavs[n.id].flow.toFixed(1) + ' m³/m' } };
      }
      if (n.type === 'ahu') {
          return { ...n, data: { ...n.data, pressure: newSimData.ahuPressure } };
      }
      return n;
    }));

    setEdges(eds => eds.map(e => {
      if (!e.data?.isFlow) return e;
      const isFault = currentScenario === 'fault';
      const isRem = currentScenario === 'remediating';
      const gradientId = isFault ? 'flow-fault' : (isRem ? 'flow-rem' : 'flow-nominal');
      const markerColor = isFault ? 'var(--accent-red)' : (isRem ? 'var(--accent-yellow)' : 'var(--accent-green)');
      
      return {
         ...e,
         className: !isFault ? 'edge-flow-vector-fast' : 'edge-flow-vector',
         style: { ...e.style, stroke: `url(#${gradientId})`, strokeDasharray: isFault ? '4 4' : '5 5' },
         markerEnd: { type: MarkerType.ArrowClosed, color: markerColor }
      };
    }));
  }, []);

  const {
    simData,
    initialData,
    activeScenario,
    autoPilot,
    setAutoPilot,
    faultTarget,
    setFaultTarget,
    loadHistory,
    globalMetrics,
    loadScenario
  } = useDigitalTwin(onSimUpdate);

  const executeRemediation = () => {
    setShowAiModal(false);
    loadScenario('remediating');
    setTimeout(() => {
      loadScenario('peak');
    }, 8000);
  };

  // When activeFloor changes or ontology loads, completely rebuild the topology
  useEffect(() => {
    activeFloorRef.current = activeFloor;
    // NOTE: In the original App.jsx, buildTopologyFromSim needs simData.
    // We pass simData to it to build initial nodes
    // Wait, buildTopologyFromSim is defined below this component? Yes.
    // We can just use simData directly.
    const topo = buildTopologyFromSim(simData, activeFloor, ontology);
    setNodes(topo.nodes);
    setEdges(topo.edges);
  }, [activeFloor, ontology]); // Only rebuild on floor or ontology change

"""
content = re.sub(target_pattern, replacement, content, flags=re.DOTALL)

with open('src/App.jsx', 'w') as f:
    f.write(content)
