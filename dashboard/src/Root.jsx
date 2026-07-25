import React, { useState, useEffect } from 'react';
import App from './App';
import MobileApp from './MobileApp';
import ErrorBoundary from './ErrorBoundary';
import HardwareInspector from './HardwareInspector'; // TEMPORARY bring-up module

export default function Root() {
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

  // TEMPORARY: ?inspector (or #inspector) swaps the console for the hardware inspector.
  // A separate page rather than a panel, so chasing a loose wire does not also load the
  // 3D building — and so the whole module is removable without touching App.jsx.
  const inspector = typeof window !== 'undefined' &&
    (window.location.search.includes('inspector') || window.location.hash.includes('inspector'));

  useEffect(() => {
    const handleResize = () => {
      setIsMobile(window.innerWidth < 768);
    };
    
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // One boundary around the whole tree: a throw in any panel shows what broke instead of
  // unmounting everything and leaving a white screen that looks like the engine is down.
  return (
    <ErrorBoundary>
      {inspector ? <HardwareInspector /> : isMobile ? <MobileApp /> : <App />}
    </ErrorBoundary>
  );
}
