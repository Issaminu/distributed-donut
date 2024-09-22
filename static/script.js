window.onload = function () {
  const donut = document.getElementById("donut");

  // Credit to VB-17 for the origin of the following JS implementation of the spinning donut: https://github.com/GarvitSinghh/Donuts/blame/main/Donuts/donut.js
  const asciiframe = function (frameNumber) {
    const A = 1 + 0.07 * frameNumber;
    const B = 1 + 0.03 * frameNumber;

    const b = [];
    const z = [];

    const cA = Math.cos(A),
      sA = Math.sin(A),
      cB = Math.cos(B),
      sB = Math.sin(B);

    for (let k = 0; k < 1760; k++) {
      b[k] = k % 80 === 79 ? "\n" : " ";
      z[k] = 0;
    }

    for (let j = 0; j < 6.28; j += 0.07) {
      const ct = Math.cos(j),
        st = Math.sin(j);
      for (let i = 0; i < 6.28; i += 0.02) {
        const sp = Math.sin(i),
          cp = Math.cos(i),
          h = ct + 2,
          D = 1 / (sp * h * sA + st * cA + 5),
          t = sp * h * cA - st * sA;

        const x = 0 | (40 + 30 * D * (cp * h * cB - t * sB)),
          y = 0 | (12 + 15 * D * (cp * h * sB + t * cB)),
          o = x + 80 * y,
          N =
            0 |
            (8 *
              ((st * sA - sp * ct * cA) * cB -
                sp * ct * sA -
                st * cA -
                cp * ct * sB));

        if (y < 22 && y >= 0 && x >= 0 && x < 79 && D > z[o]) {
          z[o] = D;
          b[o] = ".,-~:;=!*#$@"[N > 0 ? N : 0];
        }
      }
    }
    return b.join("");
  };

  class CircularBuffer {
    constructor(bufferLength) {
      this.buffer = new Array(bufferLength);
      this.head = 0;
      this.size = 0;
      this.bufferLength = bufferLength;
    }

    push(item) {
      if (this.size === this.bufferLength) {
        // Buffer is full, overwrite the oldest item
        this.head = (this.head + 1) % this.bufferLength;
      } else {
        this.size++;
      }

      const insertIndex = (this.head + this.size - 1) % this.bufferLength;
      this.buffer[insertIndex] = item;
    }

    get(index) {
      if (index < 0 || index >= this.size) {
        return null;
      }
      const actualIndex = (this.head + index) % this.bufferLength;
      return this.buffer[actualIndex];
    }
  }

  function createFrames(startFrame, endFrame) {
    const frames = new Array(endFrame - startFrame);

    for (let i = startFrame; i < endFrame; i++) {
      frames[i - startFrame] = asciiframe(i);
    }
    return frames;
  }

  function drawFramesToCanvas() {
    let i = 0;
    setInterval(() => {
      donut.innerHTML = frameBuffer.get(i);
      i = Math.floor((i + 1) % frameBuffer.size);
    }, 59);
  }

  const possibleCharacters = ".,-~:;=!*#$@ \n";

  // Encoding frames
  function encodeFrames(frames) {
    return frames.flatMap((frame) => {
      const bytes = [];
      for (let i = 0; i < frame.length; i += 2) {
        const char1 = frame[i];
        const char2 = i + 1 < frame.length ? frame[i + 1] : 12; // possibleCharacters[12]=space ; Used here if there's no second character

        const index1 = possibleCharacters.indexOf(char1);
        const index2 = possibleCharacters.indexOf(char2);

        if (index1 === -1 || index2 === -1) {
          throw new Error(`Invalid character found: ${char1} or ${char2}`);
        }
        // console.log("index1: " + index1 + " index2: " + index2);

        const byte = (index1 << 4) | index2;
        bytes.push(byte);
      }
      return bytes;
    });
  }

  // Decoding frames
  function decodeFrames(encodedFrames) {
    let currentFrame = "";
    let decodedFrames = [];

    encodedFrames.forEach((byte) => {
      const highNibble = (byte >> 4) & 0x0f;
      const lowNibble = byte & 0x0f;

      const char1 = possibleCharacters[highNibble];
      const char2 = possibleCharacters[lowNibble];

      if (char1 === undefined || char2 === undefined) {
        throw new Error(`Invalid nibble value: ${highNibble} or ${lowNibble}`);
      }

      currentFrame += char1 + char2;

      if (currentFrame.length === 1760) {
        decodedFrames.push(currentFrame);
        currentFrame = "";
      }
    });

    if (currentFrame.length > 0) {
      decodedFrames.push(currentFrame);
    }

    return decodedFrames;
  }

  const ws = new WebSocket("ws://localhost:8080/connect");
  ws.binaryType = "arraybuffer";
  ws.onopen = function () {
    console.log("Connection established");
  };
  const frameBuffer = new CircularBuffer(12 * 60); // 12 seconds of animation

  ws.onmessage = function (e) {
    const data = e.data;

    const dv = new DataView(data);

    const messageType = dv.getUint8(0);

    if (messageType === 0x0) {
      // Received work request message

      console.log("Received work request");

      const startFrame = dv.getUint32(1);
      const endFrame = dv.getUint32(5);

      console.log(
        "Server requested work from " + startFrame + " to " + endFrame
      );
      const work = createFrames(startFrame, endFrame);
      const encodedData = encodeFrames(work);
      console.log("111-> Encoded work: " + work.length);

      // const decodedFrames = decodeFrames(encodedData);
      // console.log("Decoded frames: " + decodedFrames);

      console.log("Work done, sending to server...");
      ws.send(encodedData);
    } else if (messageType === 0x2) {
      // Received frame chunk broadcast message
      console.log("Received frame chunk broadcast. Drawing to canvas...");

      const encodedData = new Uint8Array(data, 1); // Skip the message type byte
      // console.log("222-> Encoded data: " + encodedData);

      // console.log(encodedData);

      const decodedFrames = decodeFrames(encodedData);
      // console.log("Decoded frames: ")
      // console.log(decodedFrames);

      // Add the decoded frames to the circular buffer
      decodedFrames.forEach((frame) => frameBuffer.push(frame));

      // Start drawing the frames to the canvas
      drawFramesToCanvas();
    } else {
      console.log("Received other message");
    }
  };
  ws.onclose = function () {
    console.log("Connection closed");
  };
};
