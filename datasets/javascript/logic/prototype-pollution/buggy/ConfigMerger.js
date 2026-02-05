class ConfigMerger {
  constructor(defaultConfig) {
    this.config = defaultConfig || {};
  }

  // BUG: Prototype Pollution
  // An attacker can send __proto__ as a key to modify Object.prototype
  merge(payload) {
    for (const key in payload) {
      if (payload.hasOwnProperty(key)) {
        if (typeof payload[key] === 'object' && payload[key] !== null) {
          if (!this.config[key]) {
            this.config[key] = {};
          }
          this.mergeRecursive(this.config[key], payload[key]);
        } else {
          this.config[key] = payload[key];
        }
      }
    }
    return this.config;
  }

  mergeRecursive(target, source) {
    for (const key in source) {
      if (typeof source[key] === 'object' && source[key] !== null) {
        if (!target[key]) target[key] = {};
        this.mergeRecursive(target[key], source[key]);
      } else {
        target[key] = source[key];
      }
    }
    return target;
  }
}

module.exports = ConfigMerger;
