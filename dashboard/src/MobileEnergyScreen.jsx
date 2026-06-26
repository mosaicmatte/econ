import React, { useState } from 'react';
import { X, ChevronDown, Calendar } from 'lucide-react';
import {
  AreaChart, Area, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend
} from 'recharts';

export default function MobileEnergyScreen({ simData, onClose }) {
  const [timeView, setTimeView] = useState('Day');

  // Dummy data for the day view
  const dayData = [
    { time: '12 AM', hvac: 0.5, total: 1.2, bess: 0, grid: 1.2 },
    { time: '4 AM', hvac: 0.4, total: 1.1, bess: 0, grid: 1.1 },
    { time: '8 AM', hvac: 1.5, total: 2.0, bess: 0.5, grid: 1.5 },
    { time: '12 PM', hvac: 2.5, total: 6.0, bess: 1.5, grid: 4.5 },
    { time: '4 PM', hvac: 2.0, total: 4.5, bess: 1.0, grid: 3.5 },
    { time: '8 PM', hvac: 1.0, total: 2.5, bess: 0, grid: 2.5 },
  ];

  // Dummy data for month view
  const monthData = [
    { date: 'May 1', hvac: 10, total: 30, bess: 5, grid: 25 },
    { date: 'May 8', hvac: 8, total: 28, bess: 4, grid: 24 },
    { date: 'May 15', hvac: 15, total: 35, bess: 8, grid: 27 },
    { date: 'May 22', hvac: 12, total: 32, bess: 6, grid: 26 },
  ];

  const isDay = timeView === 'Day';
  const data = isDay ? dayData : monthData;

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', color: '#ffffff', background: '#000000', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif', padding: '20px', minHeight: '100dvh' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '24px' }}>
        <h2 style={{ margin: 0, fontSize: '24px', fontWeight: '600' }}>Energy</h2>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
          <div style={{ display: 'flex', background: 'rgba(255,255,255,0.06)', borderRadius: '999px', padding: '3px' }}>
            {['Day', 'Month', 'Year', 'Lifetime'].map(v => (
              <button
                key={v}
                onClick={() => setTimeView(v)}
                style={{
                  background: timeView === v ? '#ffffff' : 'transparent',
                  color: timeView === v ? '#000000' : 'rgba(255,255,255,0.6)',
                  border: 'none',
                  borderRadius: '999px',
                  padding: '6px 12px',
                  fontSize: '13px',
                  fontWeight: '600',
                  cursor: 'pointer'
                }}
              >
                {v}
              </button>
            ))}
          </div>
          <button onClick={onClose} style={{ background: 'rgba(255,255,255,0.08)', border: 'none', borderRadius: '50%', padding: '8px', color: '#ffffff', display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
            <X size={20} />
          </button>
        </div>
      </div>

      <div style={{ display: 'flex', gap: '40px', marginBottom: '20px' }}>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontSize: '13px', color: '#888', fontWeight: '500' }}>Supplied</span>
          <span style={{ fontSize: '24px', fontWeight: 'bold' }}>35.3 kWh</span>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontSize: '13px', color: '#888', fontWeight: '500' }}>Consumed</span>
          <span style={{ fontSize: '24px', fontWeight: 'bold' }}>35.3 kWh</span>
        </div>
      </div>

      <div style={{ height: '300px', width: '100%', marginBottom: '30px' }}>
        <ResponsiveContainer width="100%" height="100%">
          {isDay ? (
            <AreaChart data={data}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.1)" vertical={false} />
              <XAxis dataKey="time" stroke="#888" tick={{ fill: '#888', fontSize: 12 }} axisLine={false} tickLine={false} />
              <YAxis stroke="#888" tick={{ fill: '#888', fontSize: 12 }} axisLine={false} tickLine={false} />
              <Tooltip contentStyle={{ background: '#111', border: 'none', borderRadius: '8px', color: '#fff' }} />
              <Area type="monotone" dataKey="hvac" stackId="1" stroke="#F5C242" fill="#F5C242" fillOpacity={0.6} />
              <Area type="monotone" dataKey="bess" stackId="1" stroke="#3DDC84" fill="#3DDC84" fillOpacity={0.6} />
            </AreaChart>
          ) : (
            <BarChart data={data}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.08)" vertical={false} />
              <XAxis dataKey="date" stroke="rgba(255,255,255,0.6)" tick={{ fill: 'rgba(255,255,255,0.6)', fontSize: 12 }} axisLine={false} tickLine={false} />
              <YAxis stroke="rgba(255,255,255,0.6)" tick={{ fill: 'rgba(255,255,255,0.6)', fontSize: 12 }} axisLine={false} tickLine={false} />
              <Tooltip contentStyle={{ background: 'rgba(255,255,255,0.08)', border: 'none', borderRadius: '8px', color: '#ffffff' }} />
              <Bar dataKey="hvac" stackId="a" fill="#F5C242" />
              <Bar dataKey="bess" stackId="a" fill="#3DDC84" />
              <Bar dataKey="grid" stackId="a" fill="#B8B8B8" />
            </BarChart>
          )}
        </ResponsiveContainer>
      </div>

      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <h3 style={{ margin: '0 0 20px 0', fontSize: '18px', fontWeight: '600' }}>Energy Flow</h3>
        
        <FlowRow label="HVAC Load" value="15.8 kWh" percentage="45%" color="#F5C242" />
        <FlowRow label="Total Load" value="35.3 kWh" percentage="100%" color="#4A90E2" />
        <FlowRow label="BESS" value="7.2 kWh" percentage="20%" color="#3DDC84" />
        <FlowRow label="Grid Power" value="28.1 kWh" percentage="80%" color="#B8B8B8" />
      </div>
    </div>
  );
}

function FlowRow({ label, value, percentage, color }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <div style={{ width: '12px', height: '12px', borderRadius: '6px', background: color }} />
        <span style={{ fontSize: '16px', fontWeight: '500' }}>{label}</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: '20px' }}>
        <span style={{ fontSize: '14px', color: '#aaa', fontWeight: '500' }}>{percentage}</span>
        <span style={{ fontSize: '16px', fontWeight: '600', width: '80px', textAlign: 'right' }}>{value}</span>
      </div>
    </div>
  );
}
