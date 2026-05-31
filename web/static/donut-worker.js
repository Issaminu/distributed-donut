// Credit to a1k0n for the origin of the spinning donut:
// https://www.a1k0n.net/2021/01/13/optimizing-donut.html

const STEP_THETA = 0.07;
const STEP_PHI = 0.02;
const cStepT = Math.cos(STEP_THETA),
  sStepT = Math.sin(STEP_THETA),
  cStepP = Math.cos(STEP_PHI),
  sStepP = Math.sin(STEP_PHI);

const asciiframe = (frameNumber) => {
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
  // ct, st track cos/sin of theta, stepped by STEP_THETA each iteration.
  let ct = 1,
    st = 0;
  for (let j = 0; j < 6.28; j += STEP_THETA) {
    // j <=> theta
    const h = ct + 2; // R1 + R2*cos(theta)
    // cp, sp track cos/sin of phi, reset to phi=0 and stepped by STEP_PHI.
    let cp = 1,
      sp = 0;
    for (let i = 0; i < 6.28; i += STEP_PHI) {
      // i <=> phi
      const D = 1 / (sp * h * sA + st * cA + 5), // this is 1/z
        t = sp * h * cA - st * sA; // this is a clever factoring of some of the terms in x' and y'

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

      // Rotate phi by STEP_PHI: [cp, sp] <- R(STEP_PHI) [cp, sp].
      const cpNext = cp * cStepP - sp * sStepP;
      sp = sp * cStepP + cp * sStepP;
      cp = cpNext;
    }

    // Rotate theta by STEP_THETA: [ct, st] <- R(STEP_THETA) [ct, st].
    const ctNext = ct * cStepT - st * sStepT;
    st = st * cStepT + ct * sStepT;
    ct = ctNext;
  }
  return b.join("");
};

function createFrames(startFrame, endFrame) {
  const frames = new Array(endFrame - startFrame + 1);

  for (let i = startFrame; i <= endFrame; i++) {
    frames[i - startFrame] = asciiframe(i);
  }
  return frames;
}

const possibleCharacters = ".,-~:;=!*#$@ \n";

const charToIndex = Object.fromEntries(
  [...possibleCharacters].map((char, index) => [char, index])
);

// Encoding frames
function encodeRenderResult(renderTaskID, frames) {
  const encodedFrames = frames.flatMap((frame) => {
    const bytes = [];
    for (let i = 0; i < frame.length; i += 2) {
      const char1 = frame[i];
      const char2 = i + 1 < frame.length ? frame[i + 1] : " "; // pad with a space when a frame has an odd character count

      const index1 = charToIndex[char1];
      const index2 = charToIndex[char2];

      if (index1 === undefined || index2 === undefined) {
        throw new Error(`Invalid character found: ${char1} or ${char2}`);
      }

      const byte = (index1 << 4) | index2;
      bytes.push(byte);
    }
    return bytes;
  });
  const result = new Uint8Array(encodedFrames.length + 5); // 1 for message type, 4 for renderTaskID, and the rest is the encoded frames

  result.set(encodedFrames, 5);
  result[0] = 0x1; // messageType: MessageTypeRenderResult
  // Big endian encoding
  result[1] = (renderTaskID >> 24) & 0xff;
  result[2] = (renderTaskID >> 16) & 0xff;
  result[3] = (renderTaskID >> 8) & 0xff;
  result[4] = renderTaskID & 0xff;
  return result;
}

self.onmessage = (e) => {
  const { renderTaskID, startFrame, endFrame } = e.data;
  const renderResult = createFrames(startFrame, endFrame);
  const encodedData = encodeRenderResult(renderTaskID, renderResult);
  self.postMessage(encodedData, [encodedData.buffer]);
};
