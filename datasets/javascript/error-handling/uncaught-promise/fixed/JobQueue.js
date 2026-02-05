const { Worker } = require('worker_threads');

class JobQueue {
  constructor() {
    this.jobs = [];
    this.isProcessing = false;
  }

  addJob(data) {
    this.jobs.push(data);
    this.process();
  }

  async process() {
    if (this.isProcessing) return;
    this.isProcessing = true;

    // FIXED: Properly handle async execution
    while (this.jobs.length > 0) {
      const job = this.jobs.shift();
      try {
        await this.runJob(job);
      } catch (err) {
        console.error(`Failed to run job ${job.id}:`, err);
      }
    }
    
    this.isProcessing = false;
  }

  async runJob(job) {
    if (Math.random() > 0.8) {
      throw new Error('Job failed');
    }
    await new Promise(r => setTimeout(r, 100));
    console.log('Job completed:', job.id);
  }
}

module.exports = JobQueue;
