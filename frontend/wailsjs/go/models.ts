export namespace main {
	
	export class DiagResult {
	    name: string;
	    status: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class PrintRequest {
	    printer: string;
	    pngBase64: string;
	    widthMM: number;
	    heightMM: number;
	    gapMM: number;
	    media: string;
	    density: number;
	    speed: number;
	    direction: number;
	    copies: number;
	    invert: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PrintRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.printer = source["printer"];
	        this.pngBase64 = source["pngBase64"];
	        this.widthMM = source["widthMM"];
	        this.heightMM = source["heightMM"];
	        this.gapMM = source["gapMM"];
	        this.media = source["media"];
	        this.density = source["density"];
	        this.speed = source["speed"];
	        this.direction = source["direction"];
	        this.copies = source["copies"];
	        this.invert = source["invert"];
	    }
	}
	export class PrinterList {
	    names: string[];
	    default: string;
	
	    static createFrom(source: any = {}) {
	        return new PrinterList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	        this.default = source["default"];
	    }
	}
	export class TemplateFile {
	    path: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new TemplateFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	    }
	}

}

