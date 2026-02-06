import React, { useEffect, useRef } from 'react';

const Modal = ({ isOpen, children }) => {
  if (!isOpen) return null;

  // It also doesn't clean up properly if the component crashes or unmounts unexpectedly.
  const el = document.createElement('div');
  el.className = 'modal-portal';
  document.body.appendChild(el);
  
  // This is a naive attempt at a portal without using ReactDOM.createPortal
  // or cleaning up correctly.
  
  return (
    <div className="modal-overlay">
      <div className="modal-content">
        {children}
      </div>
    </div>
  );
};

export default Modal;
