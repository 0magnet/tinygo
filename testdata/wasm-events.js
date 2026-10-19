const fs = require('fs')

require('../targets/wasm_exec.js');

global.throwingLength = { get length() { throw new Error('length'); } };

let go = new Go();
go.exit = (code) => console.log('exit', code);

global.ready = () => setTimeout(() => {
    try {
        crash();
    } catch (err) {
        console.log('crash:', String(err));
    }
    console.log('exited:', go.exited, go.exitCode);
    try {
        ping();
    } catch (err) {
        console.log('ping:', String(err));
    }
});

WebAssembly.instantiate(fs.readFileSync(process.argv[2]), go.importObject).then((result) => {
    go.run(result.instance).then((code) => console.log('run:', code));
}).catch((err) => {
    console.error(err);
    process.exit(1);
});
