import React from 'react';

// Wraps a WebGL <Canvas>. React Three Fiber's built-in boundary blanks the canvas
// permanently on any transient render error (e.g. a momentary undefined during a
// floor/scenario switch, or a lost WebGL context) — which is what makes the 3D view
// "black out and never come back". This boundary instead REMOUNTS the subtree after a
// short delay (incrementing a key), so a one-off error self-heals to a fresh canvas.
export default class CanvasErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { renderKey: 0, errored: false, retryCount: 0 };
    this._timer = null;
  }

  static getDerivedStateFromError() {
    return { errored: true };
  }

  componentDidCatch(error) {
    // eslint-disable-next-line no-console
    console.warn('[CanvasErrorBoundary] recovered from:', error?.message || error);
    
    // If we've retried too many times rapidly, stop and show failsafe
    if (this.state.retryCount >= 3) {
      return; 
    }

    clearTimeout(this._timer);
    this._timer = setTimeout(() => {
      this.setState((s) => ({ 
        renderKey: s.renderKey + 1, 
        errored: false, 
        retryCount: s.retryCount + 1 
      }));
      
      // Decay the retry count after a while so it doesn't permanently lock out over hours
      setTimeout(() => {
        if (this.state.retryCount > 0) {
          this.setState((s) => ({ retryCount: Math.max(0, s.retryCount - 1) }));
        }
      }, 5000);
    }, 500);
  }

  componentWillUnmount() {
    clearTimeout(this._timer);
  }

  render() {
    if (this.state.errored) {
      // Failsafe UI after max retries
      if (this.state.retryCount >= 3) {
        return (
          <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(10px)', color: '#fff', zIndex: 100, textAlign: 'center', padding: '20px', borderRadius: '8px', border: '1px solid rgba(255, 59, 48, 0.3)' }}>
            <div style={{ marginBottom: '16px', background: 'rgba(255, 59, 48, 0.2)', padding: '12px', borderRadius: '50%' }}>
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#FF3B30" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>
            </div>
            <h3 style={{ fontSize: '16px', fontWeight: '600', margin: '0 0 8px' }}>Visualizer Offline</h3>
            <p style={{ fontSize: '13px', color: 'rgba(255,255,255,0.6)', margin: '0 0 20px', maxWidth: '250px' }}>The 3D engine encountered a critical fault. Live data and telemetry remain active.</p>
            <button 
              onClick={() => this.setState({ renderKey: this.state.renderKey + 1, errored: false, retryCount: 0 })}
              style={{ background: '#4A90E2', border: 'none', padding: '8px 16px', borderRadius: '6px', color: '#fff', fontSize: '12px', fontWeight: '600', cursor: 'pointer', transition: 'background 0.2s' }}
            >
              Reboot Engine
            </button>
          </div>
        );
      }
      // brief, unobtrusive placeholder while we remount
      return this.props.fallback ?? null;
    }
    // display:contents so this wrapper adds no layout box around the canvas
    return (
      <div style={{ display: 'contents' }} key={this.state.renderKey}>
        {this.props.children}
      </div>
    );
  }
}
