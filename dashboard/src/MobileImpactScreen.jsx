import React from 'react';
import { X } from 'lucide-react';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip } from 'recharts';

export default function MobileImpactScreen({ simData, onClose }) {
  const gridIndependenceData = [
    { name: 'BESS', value: 80, color: '#3DDC84' },
    { name: 'Grid', value: 20, color: '#B8B8B8' },
  ];

  const timeOfUseData = [
    { time: 'Off-Peak', kwh: 12, fill: '#3DDC84' },
    { time: 'Shoulder', kwh: 5, fill: '#F5C242' },
    { time: 'Peak', kwh: 2, fill: '#FF3B30' },
  ];

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', color: '#ffffff', background: '#000000', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif', padding: '20px', minHeight: '100dvh', overflowY: 'auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '30px' }}>
        <h2 style={{ margin: 0, fontSize: '24px', fontWeight: '600' }}>Impact</h2>
        <button onClick={onClose} style={{ background: 'rgba(255,255,255,0.08)', border: 'none', borderRadius: '50%', padding: '8px', color: '#ffffff', display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
          <X size={20} />
        </button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
        
        {/* Grid Independence Card */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Grid Independence</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>How much of the building load is powered by BESS instead of the grid.</div>
          
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', position: 'relative', height: '200px' }}>
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={gridIndependenceData}
                  innerRadius={65}
                  outerRadius={80}
                  paddingAngle={2}
                  dataKey="value"
                  stroke="none"
                >
                  {gridIndependenceData.map((entry, index) => (
                    <Cell key={`cell-${index}`} fill={entry.color} />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
            <div style={{ position: 'absolute', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
              <span style={{ fontSize: '32px', fontWeight: 'bold' }}>80%</span>
              <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>Independent</span>
            </div>
          </div>
        </div>

        {/* Peak Shaving Card */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Peak Shaving</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>BESS discharge vs Peak Load demand.</div>
          <div style={{ fontSize: '32px', fontWeight: 'bold', marginBottom: '16px' }}>125%</div>
          <div style={{ height: '24px', background: 'rgba(255,255,255,0.06)', borderRadius: '12px', overflow: 'hidden', display: 'flex' }}>
             <div style={{ width: '80%', background: '#3DDC84' }} />
             <div style={{ width: '20%', background: '#FF3B30' }} />
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: '8px', fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>
             <span>BESS Discharged: 35.3 kWh</span>
             <span>Total Demand: 28.1 kWh</span>
          </div>
        </div>

        {/* Time of Use Card */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Time-of-Use</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>Energy breakdown by time period.</div>
          
          <div style={{ height: '150px' }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={timeOfUseData} layout="vertical" margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                <XAxis type="number" hide />
                <YAxis dataKey="time" type="category" stroke="rgba(255,255,255,0.6)" tick={{ fill: 'rgba(255,255,255,0.6)', fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip cursor={{ fill: 'rgba(255,255,255,0.05)' }} contentStyle={{ background: 'rgba(255,255,255,0.08)', border: 'none', borderRadius: '8px', color: '#ffffff' }} />
                <Bar dataKey="kwh" radius={[0, 4, 4, 0]} barSize={20} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>

      </div>
      <div style={{ height: '40px' }} />
    </div>
  );
}
