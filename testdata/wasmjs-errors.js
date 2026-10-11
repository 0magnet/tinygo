const fs = require('fs')

require('../targets/wasm_exec.js');

global.throwing = new Proxy({}, {
    get() { throw new Error('get'); },
    set() { throw new Error('set'); },
    deleteProperty() { throw new Error('delete'); },
});

let go = new Go();
WebAssembly.instantiate(fs.readFileSync(process.argv[2]), go.importObject).then((result) => {
    go.run(result.instance).then((code) => console.log('run:', code));
}).catch((err) => {
    console.error(err);
    process.exit(1);
});
