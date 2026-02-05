import React, { useState, useEffect } from 'react';
import { WebSocket } from './websocket';

const LiveFeed = ({ feedId }) => {
  const [messages, setMessages] = useState([]);

  useEffect(() => {
    const ws = new WebSocket(`ws://api.example.com/feed/${feedId}`);
    
    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      setMessages(prev => [...prev, data]);
    };

    ws.connect();

    // BUG: Missing cleanup function
    // When component unmounts or feedId changes, the connection remains open.
    // This causes memory leaks and multiple subscriptions.

  }, [feedId]);

  return (
    <div className="feed">
      {messages.map(msg => (
        <div key={msg.id}>{msg.text}</div>
      ))}
    </div>
  );
};

export default LiveFeed;
